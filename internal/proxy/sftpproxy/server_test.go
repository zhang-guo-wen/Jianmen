package sftpproxy

import (
	"os"
	"testing"

	"github.com/pkg/sftp"
)

func TestOSOpenFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags sftp.FileOpenFlags
		want  int
	}{
		{name: "default read only", flags: sftp.FileOpenFlags{}, want: os.O_RDONLY},
		{name: "read", flags: sftp.FileOpenFlags{Read: true}, want: os.O_RDONLY},
		{name: "write", flags: sftp.FileOpenFlags{Write: true}, want: os.O_WRONLY},
		{name: "read write", flags: sftp.FileOpenFlags{Read: true, Write: true}, want: os.O_RDWR},
		{
			name:  "create truncate exclusive",
			flags: sftp.FileOpenFlags{Write: true, Creat: true, Trunc: true, Excl: true},
			want:  os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_EXCL,
		},
		{
			name:  "read while creating",
			flags: sftp.FileOpenFlags{Read: true, Creat: true},
			want:  os.O_RDWR | os.O_CREATE,
		},
		{name: "append is writable", flags: sftp.FileOpenFlags{Append: true}, want: os.O_WRONLY},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := osOpenFlags(test.flags); got != test.want {
				t.Fatalf("osOpenFlags(%#v) = %#x, want %#x", test.flags, got, test.want)
			}
		})
	}
}

func TestOSOpenFlagsDoesNotUseAppendMode(t *testing.T) {
	got := osOpenFlags(sftp.FileOpenFlags{Append: true, Creat: true})

	if got&os.O_APPEND != 0 {
		t.Fatalf("append request enabled os.O_APPEND: flags = %#x", got)
	}
	if got&os.O_WRONLY == 0 || got&os.O_CREATE == 0 {
		t.Fatalf("append request is not writable and creatable: flags = %#x", got)
	}
}
