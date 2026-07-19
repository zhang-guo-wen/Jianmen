//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/server/dbproxy"
	jmstore "jianmen/internal/store"
	"jianmen/internal/util"
)

const (
	redisUpstreamUser     = "default"
	redisUpstreamPassword = "redis-upstream-password"
)

func TestDatabaseGatewayRedisAgainstDocker(t *testing.T) {
	requireDocker(t)

	for _, image := range redisImages() {
		image := image
		t.Run(sanitizeTestName(image), func(t *testing.T) {
			containerID := runContainer(
				t,
				"jianmen-it-redis",
				"-p", "127.0.0.1::6379",
				image,
				"redis-server",
				"--save", "",
				"--appendonly", "no",
				"--requirepass", redisUpstreamPassword,
			)
			upstreamAddr := containerAddress(t, containerID, "6379/tcp")
			waitRedis(t, upstreamAddr)
			assertRedisServerVersion(t, upstreamAddr, image)

			fixture := newMetadataFixture(t)
			host, port := splitAddress(t, upstreamAddr)
			instance, err := fixture.store.AddDatabaseInstance(jmstore.DatabaseInstanceInput{
				Name: "docker-redis-" + sanitizeTestName(image), Protocol: "redis",
				Address: host, Port: port, TLSMode: "disable",
			})
			if err != nil {
				t.Fatalf("add Redis instance: %v", err)
			}
			account, err := fixture.store.AddDatabaseAccount(
				instance.ID,
				redisUpstreamUser,
				redisUpstreamPassword,
				"",
				"",
				nil,
			)
			if err != nil {
				t.Fatalf("add Redis account: %v", err)
			}
			compactUsername := util.PrefixRedis + account.ResourceID + fixture.session.SessionID

			for _, mode := range databaseGatewayModes() {
				mode := mode
				t.Run(mode, func(t *testing.T) {
					gateway := startRedisDatabaseGateway(t, fixture, mode)

					resp2 := newRedisGatewayProtocolClient(t, gateway)
					resp2.authenticateRESP2(t, compactUsername, integrationPassword)
					exerciseRedisCommonCompatibility(t, resp2, "resp2", 2)
					exerciseRedisPubSubCompatibility(t, resp2, gateway, upstreamAddr, compactUsername, 2)
					resp2.close(t)

					hello2 := newRedisGatewayProtocolClient(t, gateway)
					hello2.authenticateRESP2ViaHELLO(t, compactUsername, integrationPassword)
					hello2.expect(t, hello2.command(t, "PING"), '+', "PONG")
					hello2.close(t)

					resp3 := newRedisGatewayProtocolClient(t, gateway)
					resp3.authenticateRESP3(t, compactUsername, integrationPassword)
					exerciseRedisCommonCompatibility(t, resp3, "resp3", 3)
					exerciseRedisRESP3Types(t, resp3)
					exerciseRedisPubSubCompatibility(t, resp3, gateway, upstreamAddr, compactUsername, 3)
					resp3.close(t)

					assertDBAuditSQLContains(t, fixture.replayDir, "SET resp2:value [REDACTED]")
					assertDBAuditSQLContains(t, fixture.replayDir, "SET resp3:value [REDACTED]")
				})
			}
		})
	}
}

func redisImages() []string {
	raw := strings.TrimSpace(os.Getenv("JIANMEN_REDIS_IMAGES"))
	if raw == "" {
		return []string{"redis:6.2-alpine", "redis:7.4-alpine", "redis:8.8-alpine"}
	}
	var images []string
	for _, item := range strings.Split(raw, ",") {
		if image := strings.TrimSpace(item); image != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return []string{"redis:6.2-alpine", "redis:7.4-alpine", "redis:8.8-alpine"}
	}
	return images
}

func startRedisDatabaseGateway(
	t *testing.T,
	fixture metadataFixture,
	mode string,
) databaseGatewayEndpoint {
	t.Helper()
	address := freeTCPAddress(t)
	certFile, keyFile, caFile := writeIntegrationTLSCertificate(t)
	cfg := config.DatabaseGatewayConfig{Enabled: true}
	configureDatabaseGatewayMode(
		t,
		&cfg,
		mode,
		"redis",
		config.DatabaseProtocolListener{
			Enabled:    true,
			Address:    address,
			CertFile:   certFile,
			KeyFile:    keyFile,
			CAFile:     caFile,
			ServerName: "127.0.0.1",
		},
	)
	gateway := dbproxy.NewGateway(
		cfg,
		fixture.store,
		fixture.replayDir,
		testLogger(),
		fixture.db,
		newIntegrationAuthorizer(t, fixture),
		online.NewRegistry(),
		fixture.store,
	)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- gateway.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Redis gateway stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Redis gateway did not stop")
		}
	})
	waitServerTCP(t, address, errCh)
	return databaseGatewayEndpoint{address: address, caFile: caFile}
}

func waitRedis(t *testing.T, address string) {
	t.Helper()
	waitFor(t, 2*time.Minute, 250*time.Millisecond, func() error {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		client := &redisProtocolClient{conn: conn, reader: bufio.NewReader(conn)}
		if err := client.write("AUTH", redisUpstreamUser, redisUpstreamPassword); err != nil {
			return err
		}
		response, err := client.read()
		if err != nil || response.kind != '+' {
			return fmt.Errorf("Redis AUTH response kind=%q err=%v", response.kind, err)
		}
		if err := client.write("PING"); err != nil {
			return err
		}
		response, err = client.read()
		if err != nil || response.kind != '+' || response.text != "PONG" {
			return fmt.Errorf("Redis PING response=%#v err=%v", response, err)
		}
		return nil
	})
}

func assertRedisServerVersion(t *testing.T, address, image string) {
	t.Helper()
	expected := ""
	for _, version := range []string{"6.2", "7.4", "8.8"} {
		if strings.Contains(image, ":"+version) {
			expected = version + "."
			break
		}
	}
	if expected == "" {
		t.Fatalf("Redis image %q has no expected version mapping", image)
	}
	client := newRedisProtocolClient(t, address)
	client.expect(t, client.command(t, "AUTH", redisUpstreamUser, redisUpstreamPassword), '+', "OK")
	info := client.command(t, "INFO", "server")
	client.close(t)
	if info.kind != '$' || !strings.Contains(info.text, "\nredis_version:"+expected) {
		t.Fatalf("Redis image %q reported unexpected INFO server version: %q", image, info.text)
	}
}

func exerciseRedisCommonCompatibility(t *testing.T, client *redisProtocolClient, prefix string, version int) {
	t.Helper()
	client.expect(t, client.command(t, "PING"), '+', "PONG")
	client.expect(t, client.command(t, "SET", prefix+":value", "hello"), '+', "OK")
	client.expect(t, client.command(t, "GET", prefix+":value"), '$', "hello")

	client.writePipeline(t,
		[]string{"INCR", prefix + ":pipeline"},
		[]string{"INCR", prefix + ":pipeline"},
		[]string{"GET", prefix + ":pipeline"},
	)
	client.expectInteger(t, client.readValue(t), 1)
	client.expectInteger(t, client.readValue(t), 2)
	client.expect(t, client.readValue(t), '$', "2")

	client.writePipeline(t,
		[]string{"MULTI"},
		[]string{"SET", prefix + ":transaction", "committed"},
		[]string{"GET", prefix + ":transaction"},
		[]string{"EXEC"},
	)
	client.expect(t, client.readValue(t), '+', "OK")
	client.expect(t, client.readValue(t), '+', "QUEUED")
	client.expect(t, client.readValue(t), '+', "QUEUED")
	exec := client.readValue(t)
	if exec.kind != '*' || len(exec.values) != 2 || exec.values[0].text != "OK" || exec.values[1].text != "committed" {
		t.Fatalf("EXEC response = %#v", exec)
	}

	client.expect(t, client.command(t, "SELECT", "1"), '+', "OK")
	client.expect(t, client.command(t, "SET", prefix+":selected", "db-one"), '+', "OK")
	client.expect(t, client.command(t, "SELECT", "0"), '+', "OK")
	missing := client.command(t, "GET", prefix+":selected")
	if version == 2 {
		if missing.kind != '$' || !missing.null {
			t.Fatalf("RESP2 missing value = %#v", missing)
		}
	} else if missing.kind != '_' || !missing.null {
		t.Fatalf("RESP3 missing value = %#v", missing)
	}
	client.expect(t, client.command(t, "SELECT", "1"), '+', "OK")
	client.expect(t, client.command(t, "GET", prefix+":selected"), '$', "db-one")
	client.expect(t, client.command(t, "SELECT", "0"), '+', "OK")

	large := client.command(t, "EVAL", "return string.rep('x', 300000)", "0")
	if large.kind != '$' || len(large.text) != 300000 {
		t.Fatalf("large Redis response kind=%q length=%d", large.kind, len(large.text))
	}
}

func exerciseRedisRESP3Types(t *testing.T, client *redisProtocolClient) {
	t.Helper()
	client.expectInteger(t, client.command(t, "HSET", "resp3:hash", "field", "value"), 1)
	hash := client.command(t, "HGETALL", "resp3:hash")
	if hash.kind != '%' || len(hash.values) != 2 || hash.values[0].text != "field" || hash.values[1].text != "value" {
		t.Fatalf("RESP3 map = %#v", hash)
	}
	client.expectInteger(t, client.command(t, "SADD", "resp3:set", "member"), 1)
	set := client.command(t, "SMEMBERS", "resp3:set")
	if set.kind != '~' || len(set.values) != 1 || set.values[0].text != "member" {
		t.Fatalf("RESP3 set = %#v", set)
	}
	client.expectInteger(t, client.command(t, "SISMEMBER", "resp3:set", "member"), 1)
	boolean := client.command(t, "EVAL", "redis.setresp(3); return true", "0")
	if boolean.kind != '#' || boolean.text != "t" {
		t.Fatalf("RESP3 boolean = %#v", boolean)
	}
	client.expectInteger(t, client.command(t, "ZADD", "resp3:zset", "1.5", "member"), 1)
	double := client.command(t, "ZSCORE", "resp3:zset", "member")
	if double.kind != ',' || double.text != "1.5" {
		t.Fatalf("RESP3 double = %#v", double)
	}
}

func exerciseRedisPubSubCompatibility(
	t *testing.T,
	subscriber *redisProtocolClient,
	gateway databaseGatewayEndpoint,
	upstreamAddr string,
	compactUsername string,
	version int,
) {
	t.Helper()
	channel := "compat:channel:" + strconv.Itoa(version)
	subscriber.writeCommand(t, "SUBSCRIBE", channel)
	ack := subscriber.readValue(t)
	expectedKind := byte('*')
	if version == 3 {
		expectedKind = '>'
	}
	if ack.kind != expectedKind || len(ack.values) < 2 || ack.values[0].text != "subscribe" || ack.values[1].text != channel {
		t.Fatalf("subscribe ack = %#v", ack)
	}

	publisher := newRedisGatewayProtocolClient(t, gateway)
	publisher.authenticateRESP2(t, compactUsername, integrationPassword)
	publisher.expectInteger(t, publisher.command(t, "PUBLISH", channel, "payload"), 1)
	publisher.close(t)

	message := subscriber.readValue(t)
	if message.kind != expectedKind || len(message.values) < 3 ||
		message.values[0].text != "message" || message.values[1].text != channel || message.values[2].text != "payload" {
		t.Fatalf("pubsub message = %#v", message)
	}
	subscriber.writeCommand(t, "UNSUBSCRIBE", channel)
	unsubscribe := subscriber.readValue(t)
	if unsubscribe.kind != expectedKind || len(unsubscribe.values) < 2 || unsubscribe.values[0].text != "unsubscribe" {
		t.Fatalf("unsubscribe ack = %#v", unsubscribe)
	}

	first := channel + ":a"
	second := channel + ":b"
	subscriber.writePipeline(t,
		[]string{"SUBSCRIBE", first, second},
		[]string{"UNSUBSCRIBE"},
	)
	for index, topic := range []string{first, second} {
		ack := subscriber.readValue(t)
		if ack.kind != expectedKind || len(ack.values) < 2 ||
			ack.values[0].text != "subscribe" || ack.values[1].text != topic {
			t.Fatalf("pipelined subscribe ack %d = %#v, want %s", index, ack, topic)
		}
	}
	unsubscribed := make(map[string]struct{}, 2)
	for index := 0; index < 2; index++ {
		ack := subscriber.readValue(t)
		if ack.kind != expectedKind || len(ack.values) < 2 || ack.values[0].text != "unsubscribe" {
			t.Fatalf("pipelined unsubscribe ack %d = %#v", index, ack)
		}
		unsubscribed[ack.values[1].text] = struct{}{}
	}
	if _, ok := unsubscribed[first]; !ok {
		t.Fatalf("pipelined unsubscribe acknowledgements missing %q: %v", first, unsubscribed)
	}
	if _, ok := unsubscribed[second]; !ok {
		t.Fatalf("pipelined unsubscribe acknowledgements missing %q: %v", second, unsubscribed)
	}
	subscriber.expect(t, subscriber.command(t, "PING"), '+', "PONG")

	boundaryChannel := strings.Repeat("b", 65507)
	subscriber.writeCommand(t, "SUBSCRIBE", boundaryChannel)
	boundarySubscribe := subscriber.readValue(t)
	if boundarySubscribe.kind != expectedKind || len(boundarySubscribe.values) < 2 ||
		boundarySubscribe.values[0].text != "subscribe" ||
		boundarySubscribe.values[1].text != boundaryChannel {
		t.Fatalf("boundary subscribe ack = %#v", boundarySubscribe)
	}
	directPublisher := newRedisProtocolClient(t, upstreamAddr)
	directPublisher.expect(
		t,
		directPublisher.command(t, "AUTH", redisUpstreamUser, redisUpstreamPassword),
		'+',
		"OK",
	)
	directPublisher.expectInteger(
		t,
		directPublisher.command(t, "PUBLISH", boundaryChannel, "boundary-payload"),
		1,
	)
	directPublisher.close(t)
	boundaryMessage := subscriber.readValue(t)
	if boundaryMessage.kind != expectedKind || len(boundaryMessage.values) < 3 ||
		boundaryMessage.values[0].text != "message" ||
		boundaryMessage.values[1].text != boundaryChannel ||
		boundaryMessage.values[2].text != "boundary-payload" {
		t.Fatalf("boundary Pub/Sub message = %#v", boundaryMessage)
	}
	subscriber.writeCommand(t, "PING")
	boundaryPONG := subscriber.readValue(t)
	if version == 2 {
		if boundaryPONG.kind != '*' || len(boundaryPONG.values) < 1 ||
			boundaryPONG.values[0].text != "pong" {
			t.Fatalf("boundary RESP2 PONG = %#v", boundaryPONG)
		}
	} else {
		subscriber.expect(t, boundaryPONG, '+', "PONG")
	}
	subscriber.writeCommand(t, "UNSUBSCRIBE")
	boundaryUnsubscribe := subscriber.readValue(t)
	if boundaryUnsubscribe.kind != expectedKind || len(boundaryUnsubscribe.values) < 2 ||
		boundaryUnsubscribe.values[0].text != "unsubscribe" ||
		boundaryUnsubscribe.values[1].text != boundaryChannel {
		t.Fatalf("boundary unsubscribe ack = %#v", boundaryUnsubscribe)
	}
}

type redisProtocolClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

type redisProtocolValue struct {
	kind    byte
	text    string
	integer int64
	null    bool
	values  []redisProtocolValue
}

func newRedisProtocolClient(t *testing.T, address string) *redisProtocolClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatalf("dial Redis gateway: %v", err)
	}
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		conn.Close()
		t.Fatalf("set Redis client deadline: %v", err)
	}
	return &redisProtocolClient{conn: conn, reader: bufio.NewReader(conn)}
}

func newRedisGatewayProtocolClient(
	t *testing.T,
	gateway databaseGatewayEndpoint,
) *redisProtocolClient {
	t.Helper()
	caPEM, err := os.ReadFile(gateway.caFile)
	if err != nil {
		t.Fatalf("read Redis gateway CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("parse Redis gateway CA")
	}
	raw, err := net.DialTimeout("tcp", gateway.address, 5*time.Second)
	if err != nil {
		t.Fatalf("dial Redis gateway: %v", err)
	}
	if err := raw.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		raw.Close()
		t.Fatalf("set Redis gateway client deadline: %v", err)
	}
	secured := tls.Client(raw, &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "127.0.0.1",
	})
	if err := secured.Handshake(); err != nil {
		raw.Close()
		t.Fatalf("handshake Redis gateway TLS: %v", err)
	}
	return &redisProtocolClient{conn: secured, reader: bufio.NewReader(secured)}
}

func (c *redisProtocolClient) authenticateRESP2(t *testing.T, username, password string) {
	t.Helper()
	c.expect(t, c.command(t, "AUTH", username, password), '+', "OK")
}

func (c *redisProtocolClient) authenticateRESP2ViaHELLO(t *testing.T, username, password string) {
	t.Helper()
	response := c.command(t, "HELLO", "2", "AUTH", username, password, "SETNAME", "jianmen-resp2-integration")
	if response.kind != '*' {
		t.Fatalf("HELLO 2 response = %#v, want array", response)
	}
	if value, ok := response.mapValue("proto"); !ok || value.integer != 2 {
		t.Fatalf("HELLO 2 proto field = %#v, present=%t", value, ok)
	}
}

func (c *redisProtocolClient) authenticateRESP3(t *testing.T, username, password string) {
	t.Helper()
	response := c.command(t, "HELLO", "3", "AUTH", username, password, "SETNAME", "jianmen-integration")
	if response.kind != '%' {
		t.Fatalf("HELLO 3 response = %#v, want map", response)
	}
	if value, ok := response.mapValue("proto"); !ok || value.integer != 3 {
		t.Fatalf("HELLO 3 proto field = %#v, present=%t", value, ok)
	}
}

func (c *redisProtocolClient) command(t *testing.T, parts ...string) redisProtocolValue {
	t.Helper()
	c.writeCommand(t, parts...)
	return c.readValue(t)
}

func (c *redisProtocolClient) writeCommand(t *testing.T, parts ...string) {
	t.Helper()
	if err := c.write(parts...); err != nil {
		t.Fatalf("write Redis command %q: %v", parts[0], err)
	}
}

func (c *redisProtocolClient) write(parts ...string) error {
	_, err := io.Copy(c.conn, bytes.NewReader(redisIntegrationCommand(parts...)))
	return err
}

func (c *redisProtocolClient) writePipeline(t *testing.T, commands ...[]string) {
	t.Helper()
	var wire bytes.Buffer
	for _, command := range commands {
		wire.Write(redisIntegrationCommand(command...))
	}
	if _, err := io.Copy(c.conn, bytes.NewReader(wire.Bytes())); err != nil {
		t.Fatalf("write Redis pipeline: %v", err)
	}
}

func (c *redisProtocolClient) readValue(t *testing.T) redisProtocolValue {
	t.Helper()
	value, err := c.read()
	if err != nil {
		t.Fatalf("read Redis response: %v", err)
	}
	if value.kind == '-' || value.kind == '!' {
		t.Fatalf("Redis returned error: kind=%q message=%q", value.kind, value.text)
	}
	return value
}

func (c *redisProtocolClient) read() (redisProtocolValue, error) {
	prefix, err := c.reader.ReadByte()
	if err != nil {
		return redisProtocolValue{}, err
	}
	line, err := readRedisIntegrationLine(c.reader)
	if err != nil {
		return redisProtocolValue{}, err
	}
	value := redisProtocolValue{kind: prefix}
	switch prefix {
	case '+', '-', '!', '=', ',', '(':
		if prefix == '!' || prefix == '=' {
			return c.readBulkValue(prefix, line)
		}
		value.text = string(line)
	case ':':
		value.integer, err = strconv.ParseInt(string(line), 10, 64)
	case '#':
		value.text = string(line)
	case '_':
		if len(line) != 0 {
			err = fmt.Errorf("invalid RESP3 null")
		}
		value.null = true
	case '$':
		return c.readBulkValue(prefix, line)
	case '*', '~', '>':
		value.values, err = c.readAggregate(line, 1)
	case '%':
		value.values, err = c.readAggregate(line, 2)
	case '|':
		if _, attributeErr := c.readAggregate(line, 2); attributeErr != nil {
			return redisProtocolValue{}, attributeErr
		}
		return c.read()
	default:
		err = fmt.Errorf("unsupported RESP prefix %q", prefix)
	}
	return value, err
}

func (c *redisProtocolClient) readBulkValue(prefix byte, line []byte) (redisProtocolValue, error) {
	length, err := strconv.Atoi(string(line))
	if err != nil {
		return redisProtocolValue{}, err
	}
	value := redisProtocolValue{kind: prefix}
	if length == -1 {
		value.null = true
		return value, nil
	}
	payload := make([]byte, length+2)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return redisProtocolValue{}, err
	}
	if payload[length] != '\r' || payload[length+1] != '\n' {
		return redisProtocolValue{}, fmt.Errorf("malformed Redis bulk terminator")
	}
	value.text = string(payload[:length])
	return value, nil
}

func (c *redisProtocolClient) readAggregate(line []byte, multiplier int) ([]redisProtocolValue, error) {
	count, err := strconv.Atoi(string(line))
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, nil
	}
	values := make([]redisProtocolValue, 0, count*multiplier)
	for index := 0; index < count*multiplier; index++ {
		value, err := c.read()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *redisProtocolClient) expect(t *testing.T, value redisProtocolValue, kind byte, text string) {
	t.Helper()
	if value.kind != kind || value.text != text || value.null {
		t.Fatalf("Redis response = %#v, want kind=%q text=%q", value, kind, text)
	}
}

func (c *redisProtocolClient) expectInteger(t *testing.T, value redisProtocolValue, expected int64) {
	t.Helper()
	if value.kind != ':' || value.integer != expected {
		t.Fatalf("Redis integer response = %#v, want %d", value, expected)
	}
}

func (c *redisProtocolClient) close(t *testing.T) {
	t.Helper()
	if err := c.conn.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
}

func (v redisProtocolValue) mapValue(key string) (redisProtocolValue, bool) {
	for index := 0; index+1 < len(v.values); index += 2 {
		if v.values[index].text == key {
			return v.values[index+1], true
		}
	}
	return redisProtocolValue{}, false
}

func redisIntegrationCommand(parts ...string) []byte {
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "*%d\r\n", len(parts))
	for _, part := range parts {
		fmt.Fprintf(&wire, "$%d\r\n%s\r\n", len(part), part)
	}
	return wire.Bytes()
}

func readRedisIntegrationLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("malformed Redis line")
	}
	return line[:len(line)-2], nil
}
