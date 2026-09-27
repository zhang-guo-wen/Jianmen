//go:build embedded_guacd && linux && amd64

package guacdruntime

import _ "embed"

//go:embed assets/guacd-linux-amd64.tar.gz
var archive []byte

func embeddedArchive() []byte {
	return archive
}
