package service

import (
	"context"
	"errors"
	"net"
	"testing"

	"jianmen/internal/model"
)

func TestResolveMySQLAccountHostUsesAuthenticatedConnectionLocalIP(t *testing.T) {
	for _, test := range []struct {
		name string
		ip   string
		want string
	}{
		{name: "IPv4", ip: "192.0.2.44", want: "192.0.2.44"},
		{name: "IPv6", ip: "2001:db8::44", want: "2001:db8::44"},
	} {
		t.Run(test.name, func(t *testing.T) {
			admin := model.DatabaseAccount{
				Username: "administrator",
				Password: model.NewEncryptedField("admin-secret"),
			}
			conn := &mysqlLocalAddressTestConn{
				address: &net.TCPAddr{IP: net.ParseIP(test.ip), Port: 49152},
			}
			connectCalls := 0
			host, err := resolveMySQLAccountHost(
				context.Background(),
				model.DatabaseInstance{Address: "db.example.test", Port: 3306},
				admin,
				func(
					_ context.Context,
					_ model.DatabaseInstance,
					username string,
					password string,
				) (net.Conn, error) {
					connectCalls++
					if username != admin.Username || password != "admin-secret" {
						t.Fatalf("connector credentials = %q/%q", username, password)
					}
					return conn, nil
				},
			)
			if err != nil || host != test.want {
				t.Fatalf("resolved host = %q, error = %v, want %q", host, err, test.want)
			}
			if connectCalls != 1 || !conn.closed {
				t.Fatalf("connector calls = %d, connection closed = %t", connectCalls, conn.closed)
			}
		})
	}
}

func TestResolveMySQLAccountHostRejectsNonExactLocalAddress(t *testing.T) {
	for _, test := range []struct {
		name    string
		address net.Addr
	}{
		{name: "unspecified IPv4", address: &net.TCPAddr{IP: net.IPv4zero, Port: 49152}},
		{name: "unspecified IPv6", address: &net.TCPAddr{IP: net.IPv6unspecified, Port: 49152}},
		{name: "non TCP address", address: mysqlLocalAddressTestAddr("client.example.test:49152")},
		{name: "missing address"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveMySQLAccountHost(
				context.Background(),
				model.DatabaseInstance{Address: "db.example.test", Port: 3306},
				model.DatabaseAccount{
					Username: "administrator",
					Password: model.NewEncryptedField("admin-secret"),
				},
				func(
					context.Context,
					model.DatabaseInstance,
					string,
					string,
				) (net.Conn, error) {
					return &mysqlLocalAddressTestConn{address: test.address}, nil
				},
			)
			if err == nil {
				t.Fatal("non-exact local address was accepted")
			}
		})
	}
}

func TestResolveMySQLAccountHostRequiresStoredAdministratorCredential(t *testing.T) {
	connectCalls := 0
	_, err := resolveMySQLAccountHost(
		context.Background(),
		model.DatabaseInstance{},
		model.DatabaseAccount{Username: "administrator"},
		func(
			context.Context,
			model.DatabaseInstance,
			string,
			string,
		) (net.Conn, error) {
			connectCalls++
			return nil, errors.New("unexpected connector call")
		},
	)
	if err == nil || connectCalls != 0 {
		t.Fatalf("credential error = %v, connector calls = %d", err, connectCalls)
	}
}

func TestResolveMySQLAccountHostRejectsMissingAuthenticatedConnection(t *testing.T) {
	_, err := resolveMySQLAccountHost(
		context.Background(),
		model.DatabaseInstance{},
		model.DatabaseAccount{
			Username: "administrator",
			Password: model.NewEncryptedField("admin-secret"),
		},
		func(
			context.Context,
			model.DatabaseInstance,
			string,
			string,
		) (net.Conn, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("missing authenticated connection was accepted")
	}
}

type mysqlLocalAddressTestConn struct {
	net.Conn
	address net.Addr
	closed  bool
}

func (c *mysqlLocalAddressTestConn) LocalAddr() net.Addr {
	return c.address
}

func (c *mysqlLocalAddressTestConn) Close() error {
	c.closed = true
	return nil
}

type mysqlLocalAddressTestAddr string

func (a mysqlLocalAddressTestAddr) Network() string { return "test" }
func (a mysqlLocalAddressTestAddr) String() string  { return string(a) }
