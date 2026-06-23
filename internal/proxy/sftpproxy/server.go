package sftpproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/recording"
)

type Options struct {
	Channel  io.ReadWriteCloser
	Target   *ssh.Client
	Recorder *recording.SessionRecorder
}

func Serve(ctx context.Context, opts Options) error {
	if opts.Channel == nil {
		return errors.New("sftp proxy channel is nil")
	}
	if opts.Target == nil {
		return errors.New("sftp proxy target client is nil")
	}

	remote, err := sftp.NewClient(opts.Target)
	if err != nil {
		return fmt.Errorf("open remote sftp client: %w", err)
	}
	defer remote.Close()

	handler := &handler{
		remote:   remote,
		recorder: opts.Recorder,
	}
	server := sftp.NewRequestServer(opts.Channel, sftp.Handlers{
		FileGet:  handler,
		FilePut:  handler,
		FileCmd:  handler,
		FileList: handler,
	})

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()

	err = server.Serve()
	close(done)
	if isExpectedClose(err) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type handler struct {
	remote   *sftp.Client
	recorder *recording.SessionRecorder
}

func (h *handler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	started := time.Now().UTC()
	file, err := h.remote.Open(r.Filepath)
	h.record(started, recording.FileEvent{
		Action: "open_read",
		Path:   r.Filepath,
		Detail: map[string]any{"method": r.Method},
	}, err)
	if err != nil {
		return nil, err
	}
	return h.trackedFile(file, r.Filepath), nil
}

func (h *handler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return h.openFile(r, "open_write")
}

func (h *handler) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return h.openFile(r, "open_read_write")
}

func (h *handler) openFile(r *sftp.Request, action string) (*trackedFile, error) {
	started := time.Now().UTC()
	flags := r.Pflags()
	file, err := h.remote.OpenFile(r.Filepath, osOpenFlags(flags))
	h.record(started, recording.FileEvent{
		Action: action,
		Path:   r.Filepath,
		Detail: map[string]any{
			"method": r.Method,
			"flags":  fileOpenFlagsDetail(flags),
		},
	}, err)
	if err != nil {
		return nil, err
	}
	return h.trackedFile(file, r.Filepath), nil
}

func (h *handler) Filecmd(r *sftp.Request) error {
	return h.filecmd(r, r.Method)
}

func (h *handler) PosixRename(r *sftp.Request) error {
	return h.filecmd(r, "PosixRename")
}

func (h *handler) StatVFS(r *sftp.Request) (*sftp.StatVFS, error) {
	started := time.Now().UTC()
	stat, err := h.remote.StatVFS(r.Filepath)
	h.record(started, recording.FileEvent{
		Action: "statvfs",
		Path:   r.Filepath,
		Detail: map[string]any{"method": r.Method},
	}, err)
	return stat, err
}

func (h *handler) filecmd(r *sftp.Request, method string) error {
	started := time.Now().UTC()
	path, path2 := commandPaths(method, r)
	detail := map[string]any{"method": method}
	if method == "Setstat" {
		for key, value := range setstatDetail(r) {
			detail[key] = value
		}
	}

	var err error
	switch method {
	case "Setstat":
		err = h.setstat(r)
	case "Rename":
		err = h.remote.Rename(r.Filepath, r.Target)
	case "PosixRename":
		err = h.remote.PosixRename(r.Filepath, r.Target)
	case "Rmdir":
		err = h.remote.RemoveDirectory(r.Filepath)
	case "Remove":
		err = h.remote.Remove(r.Filepath)
	case "Mkdir":
		err = h.remote.Mkdir(r.Filepath)
	case "Link":
		err = h.remote.Link(r.Filepath, r.Target)
	case "Symlink":
		err = h.remote.Symlink(r.Filepath, r.Target)
	default:
		err = sftp.ErrSSHFxOpUnsupported
	}

	h.record(started, recording.FileEvent{
		Action: methodAction(method),
		Path:   path,
		Path2:  path2,
		Detail: detail,
	}, err)
	return err
}

func (h *handler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	started := time.Now().UTC()
	var (
		files []os.FileInfo
		err   error
	)

	switch r.Method {
	case "List":
		files, err = h.remote.ReadDir(r.Filepath)
	case "Stat":
		var info os.FileInfo
		info, err = h.remote.Stat(r.Filepath)
		if err == nil {
			files = []os.FileInfo{info}
		}
	default:
		err = sftp.ErrSSHFxOpUnsupported
	}

	h.record(started, recording.FileEvent{
		Action: strings.ToLower(r.Method),
		Path:   r.Filepath,
		Size:   int64(len(files)),
		Detail: map[string]any{"method": r.Method},
	}, err)
	if err != nil {
		return nil, err
	}
	return fileInfoLister(files), nil
}

func (h *handler) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	started := time.Now().UTC()
	info, err := h.remote.Lstat(r.Filepath)
	h.record(started, recording.FileEvent{
		Action: "lstat",
		Path:   r.Filepath,
		Detail: map[string]any{"method": r.Method},
	}, err)
	if err != nil {
		return nil, err
	}
	return fileInfoLister{info}, nil
}

func (h *handler) Readlink(path string) (string, error) {
	started := time.Now().UTC()
	target, err := h.remote.ReadLink(path)
	h.record(started, recording.FileEvent{
		Action: "readlink",
		Path:   path,
		Path2:  target,
	}, err)
	return target, err
}

func (h *handler) RealPath(path string) (string, error) {
	started := time.Now().UTC()
	realPath, err := h.remote.RealPath(path)
	h.record(started, recording.FileEvent{
		Action: "realpath",
		Path:   path,
		Path2:  realPath,
	}, err)
	return realPath, err
}

func (h *handler) setstat(r *sftp.Request) error {
	flags := r.AttrFlags()
	attrs := r.Attributes()
	if flags.Size {
		if err := h.remote.Truncate(r.Filepath, int64(attrs.Size)); err != nil {
			return err
		}
	}
	if flags.Permissions {
		if err := h.remote.Chmod(r.Filepath, attrs.FileMode()); err != nil {
			return err
		}
	}
	if flags.UidGid {
		if err := h.remote.Chown(r.Filepath, int(attrs.UID), int(attrs.GID)); err != nil {
			return err
		}
	}
	if flags.Acmodtime {
		if err := h.remote.Chtimes(r.Filepath, attrs.AccessTime(), attrs.ModTime()); err != nil {
			return err
		}
	}
	if len(attrs.Extended) > 0 {
		if err := h.remote.SetExtendedData(r.Filepath, attrs.Extended); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) trackedFile(file *sftp.File, path string) *trackedFile {
	return &trackedFile{
		file:     file,
		path:     path,
		recorder: h.recorder,
	}
}

func (h *handler) record(started time.Time, event recording.FileEvent, err error) {
	recordFileEvent(h.recorder, started, event, err)
}

type trackedFile struct {
	file       *sftp.File
	path       string
	recorder   *recording.SessionRecorder
	readBytes  atomic.Int64
	writeBytes atomic.Int64
}

func (f *trackedFile) ReadAt(p []byte, off int64) (int, error) {
	started := time.Now().UTC()
	n, err := f.file.ReadAt(p, off)
	recordErr := err
	if n > 0 && errors.Is(err, io.EOF) {
		recordErr = nil
	}
	if n > 0 || recordErr != nil {
		f.readBytes.Add(int64(n))
		recordFileEvent(f.recorder, started, recording.FileEvent{
			Action: "read",
			Path:   f.path,
			Offset: off,
			Size:   int64(n),
		}, recordErr)
	}
	return n, err
}

func (f *trackedFile) WriteAt(p []byte, off int64) (int, error) {
	started := time.Now().UTC()
	n, err := f.file.WriteAt(p, off)
	if n > 0 {
		f.writeBytes.Add(int64(n))
	}
	recordFileEvent(f.recorder, started, recording.FileEvent{
		Action: "write",
		Path:   f.path,
		Offset: off,
		Size:   int64(n),
	}, err)
	return n, err
}

func (f *trackedFile) Close() error {
	started := time.Now().UTC()
	err := f.file.Close()
	recordFileEvent(f.recorder, started, recording.FileEvent{
		Action: "close",
		Path:   f.path,
		Detail: map[string]any{
			"read_bytes":  f.readBytes.Load(),
			"write_bytes": f.writeBytes.Load(),
		},
	}, err)
	return err
}

func (f *trackedFile) TransferError(err error) {
	if err == nil {
		return
	}
	recordFileEvent(f.recorder, time.Now().UTC(), recording.FileEvent{
		Action: "transfer_error",
		Path:   f.path,
	}, err)
}

type fileInfoLister []os.FileInfo

func (l fileInfoLister) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	if offset < 0 {
		return 0, os.ErrInvalid
	}
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[offset:])
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}

func osOpenFlags(flags sftp.FileOpenFlags) int {
	switch {
	case flags.Read && (flags.Write || flags.Append || flags.Creat || flags.Trunc):
		return os.O_RDWR | osMutatingOpenFlags(flags)
	case flags.Write || flags.Append || flags.Creat || flags.Trunc:
		return os.O_WRONLY | osMutatingOpenFlags(flags)
	default:
		return os.O_RDONLY
	}
}

func osMutatingOpenFlags(flags sftp.FileOpenFlags) int {
	var out int
	if flags.Creat {
		out |= os.O_CREATE
	}
	if flags.Trunc {
		out |= os.O_TRUNC
	}
	if flags.Excl {
		out |= os.O_EXCL
	}
	return out
}

func fileOpenFlagsDetail(flags sftp.FileOpenFlags) map[string]bool {
	return map[string]bool{
		"read":   flags.Read,
		"write":  flags.Write,
		"append": flags.Append,
		"create": flags.Creat,
		"trunc":  flags.Trunc,
		"excl":   flags.Excl,
	}
}

func setstatDetail(r *sftp.Request) map[string]any {
	flags := r.AttrFlags()
	attrs := r.Attributes()
	detail := map[string]any{
		"size":        flags.Size,
		"uid_gid":     flags.UidGid,
		"permissions": flags.Permissions,
		"acmodtime":   flags.Acmodtime,
	}
	if flags.Size {
		detail["new_size"] = attrs.Size
	}
	if flags.Permissions {
		detail["mode"] = attrs.FileMode().String()
	}
	if flags.UidGid {
		detail["uid"] = attrs.UID
		detail["gid"] = attrs.GID
	}
	if flags.Acmodtime {
		detail["atime"] = attrs.AccessTime().Format(time.RFC3339Nano)
		detail["mtime"] = attrs.ModTime().Format(time.RFC3339Nano)
	}
	if len(attrs.Extended) > 0 {
		detail["extended_count"] = len(attrs.Extended)
	}
	return detail
}

func commandPaths(method string, r *sftp.Request) (string, string) {
	switch method {
	case "Rename", "PosixRename":
		return r.Filepath, r.Target
	case "Link", "Symlink":
		return r.Target, r.Filepath
	default:
		return r.Filepath, ""
	}
}

func methodAction(method string) string {
	switch method {
	case "PosixRename":
		return "posix_rename"
	default:
		return strings.ToLower(method)
	}
}

func recordFileEvent(recorder *recording.SessionRecorder, started time.Time, event recording.FileEvent, err error) {
	if recorder == nil {
		return
	}
	event.StartedAt = started.UnixMilli()
	event.EndedAt = time.Now().UTC().UnixMilli()
	if err != nil {
		event.Result = "failure"
		event.Error = err.Error()
	} else {
		event.Result = "success"
	}
	recorder.RecordFileEvent(event)
}

func isExpectedClose(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}
