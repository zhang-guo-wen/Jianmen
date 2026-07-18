package mysqlwire

import (
	"bytes"
	"context"
	"errors"
	"net"
)

type AuthenticationOptions struct {
	Password       string
	VerifiedTLS    bool
	MaxPacketBytes int
}

func ContinueAuthentication(
	ctx context.Context,
	conn net.Conn,
	options AuthenticationOptions,
) error {
	if ctx == nil {
		return errors.New("continue mysql authentication: nil context")
	}
	if options.MaxPacketBytes <= 0 {
		return errors.New("continue mysql authentication: invalid packet limit")
	}
	for {
		packet, err := ReadPacket(ctx, conn, options.MaxPacketBytes)
		if err != nil {
			return err
		}
		if len(packet.Payload) == 0 {
			return errors.New("empty mysql authentication response")
		}
		switch packet.Payload[0] {
		case 0x00:
			return nil
		case 0xff:
			return errors.New("mysql authentication denied")
		case 0xfe:
			plugin, salt, ok := parseAuthSwitch(packet.Payload)
			if !ok {
				return errors.New("malformed mysql authentication switch")
			}
			response, err := BuildAuthResponse(plugin, options.Password, salt)
			if err != nil {
				return err
			}
			if err := WritePacket(ctx, conn, packet.Sequence+1, response); err != nil {
				return err
			}
		case 0x01:
			code, sequence, err := readAuthMoreData(ctx, conn, packet, options.MaxPacketBytes)
			if err != nil {
				return err
			}
			switch code {
			case 0x03:
				continue
			case 0x04:
				if !options.VerifiedTLS {
					return errors.New("mysql full authentication requires verified TLS")
				}
				password := append([]byte(options.Password), 0)
				if err := WritePacket(ctx, conn, sequence+1, password); err != nil {
					return err
				}
			default:
				return errors.New("unsupported mysql authentication continuation")
			}
		default:
			return errors.New("unexpected mysql authentication response")
		}
	}
}

func parseAuthSwitch(payload []byte) (string, []byte, bool) {
	if len(payload) < 3 || payload[0] != 0xfe {
		return "", nil, false
	}
	separator := bytes.IndexByte(payload[1:], 0)
	if separator < 0 {
		return "", nil, false
	}
	separator++
	plugin := string(payload[1:separator])
	salt := bytes.TrimSuffix(payload[separator+1:], []byte{0})
	if plugin == "" || len(salt) == 0 {
		return "", nil, false
	}
	return plugin, append([]byte(nil), salt...), true
}

func readAuthMoreData(
	ctx context.Context,
	conn net.Conn,
	packet Packet,
	maxPacketBytes int,
) (byte, byte, error) {
	switch len(packet.Payload) {
	case 2:
		return packet.Payload[1], packet.Sequence, nil
	case 1:
		continuation, err := ReadPacket(ctx, conn, maxPacketBytes)
		if err != nil {
			return 0, 0, err
		}
		if len(continuation.Payload) != 1 {
			return 0, 0, errors.New("malformed mysql authentication continuation")
		}
		return continuation.Payload[0], continuation.Sequence, nil
	default:
		return 0, 0, errors.New("malformed mysql authentication continuation")
	}
}
