//go:build embedded_guacd && (!linux || (!amd64 && !arm64))

package guacdruntime

func embeddedArchive() []byte {
	return nil
}
