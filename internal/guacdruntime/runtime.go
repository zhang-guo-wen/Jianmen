package guacdruntime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	Version            = "1.6.0"
	EmbeddedBinaryPath = "embedded"
	readyFileName      = ".jianmen-runtime-ready"
)

var ErrNotIncluded = errors.New("embedded guacd runtime is not included")

type Process struct {
	Command    string
	ArgsPrefix []string
	Env        map[string]string
	WorkDir    string
}

func Prepare(baseDir string) (Process, error) {
	archive := embeddedArchive()
	if len(archive) == 0 {
		return Process{}, fmt.Errorf(
			"%w; use the Linux RDP package or configure an external guacd binary",
			ErrNotIncluded,
		)
	}
	if runtime.GOOS != "linux" {
		return Process{}, fmt.Errorf("embedded guacd is only supported on Linux")
	}
	if baseDir == "" {
		return Process{}, fmt.Errorf("embedded guacd runtime directory is required")
	}

	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	target := filepath.Join(
		filepath.Clean(baseDir),
		fmt.Sprintf("%s-linux-%s-%s", Version, runtime.GOARCH, digest[:12]),
	)
	if err := ensureExtracted(target, archive, digest); err != nil {
		return Process{}, err
	}

	loaderName, err := loaderForArch(runtime.GOARCH)
	if err != nil {
		return Process{}, err
	}
	rootfs := filepath.Join(target, "rootfs")
	loader := filepath.Join(rootfs, "lib", loaderName)
	guacd := filepath.Join(rootfs, "opt", "guacamole", "sbin", "guacd")
	libraryPath := joinLibraryPath(rootfs)
	for _, required := range []string{loader, guacd} {
		if info, statErr := os.Stat(required); statErr != nil || info.IsDir() {
			return Process{}, fmt.Errorf("embedded guacd runtime is incomplete: %s", required)
		}
	}

	return Process{
		Command: loader,
		ArgsPrefix: []string{
			"--library-path", libraryPath,
			guacd,
		},
		Env: map[string]string{
			"LD_LIBRARY_PATH":    libraryPath,
			"FONTCONFIG_PATH":    filepath.Join(rootfs, "etc", "fonts"),
			"FONTCONFIG_SYSROOT": rootfs,
			"XDG_DATA_DIRS": filepath.Join(rootfs, "usr", "local", "share") + ":" +
				filepath.Join(rootfs, "usr", "share"),
		},
		WorkDir: filepath.Join(rootfs, "opt", "guacamole"),
	}, nil
}

func loaderForArch(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "ld-musl-x86_64.so.1", nil
	case "arm64":
		return "ld-musl-aarch64.so.1", nil
	default:
		return "", fmt.Errorf("embedded guacd does not support Linux architecture %q", arch)
	}
}

func joinLibraryPath(rootfs string) string {
	paths := []string{
		filepath.Join(rootfs, "lib"),
		filepath.Join(rootfs, "usr", "lib"),
		filepath.Join(rootfs, "usr", "local", "lib"),
		filepath.Join(rootfs, "opt", "guacamole", "lib"),
	}
	return paths[0] + ":" + paths[1] + ":" + paths[2] + ":" + paths[3]
}
