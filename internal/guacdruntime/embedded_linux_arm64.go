//go:build embedded_guacd && linux && arm64

package guacdruntime

import _ "embed"

//go:embed assets/guacd-linux-arm64.tar.gz
var archive []byte

func embeddedArchive() []byte {
	return archive
}
