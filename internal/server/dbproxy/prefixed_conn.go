package dbproxy

import "net"

// prefixedConn replays bytes consumed while detecting a client protocol before
// continuing with the protocol-specific handshake.
type prefixedConn struct {
	net.Conn
	prefix []byte
}

func (connection *prefixedConn) Read(buffer []byte) (int, error) {
	if len(connection.prefix) == 0 {
		return connection.Conn.Read(buffer)
	}
	read := copy(buffer, connection.prefix)
	connection.prefix = connection.prefix[read:]
	return read, nil
}
