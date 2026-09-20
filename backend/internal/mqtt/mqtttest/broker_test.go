package mqtttest_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/mqtt"
	"github.com/BobcGn/final/backend/internal/mqtt/mqtttest"
)

// testTimeout bounds every wait so a broken broker fails the test instead of
// hanging it.
const testTimeout = 5 * time.Second

// quietLogger discards log output.
func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// dial connects a client to the broker and runs it.
func dial(t *testing.T, broker *mqtttest.Broker, clientID string, subscriptions ...mqtt.TopicFilter) *mqtt.Client {
	t.Helper()

	client, err := mqtt.New(mqtt.Config{
		Address:       broker.Addr(),
		ClientID:      clientID,
		CleanSession:  true,
		Logger:        quietLogger(),
		ReconnectMin:  20 * time.Millisecond,
		ReconnectMax:  50 * time.Millisecond,
		Subscriptions: subscriptions,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = client.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(testTimeout):
			t.Log("the client did not stop within the timeout")
		}
	})
	return client
}

// waitFor polls a condition until it holds or the timeout expires.
func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

// TestStartAndClose verifies the lifecycle, including that closing twice is safe.
// A test cleanup that closes twice must not fail, or every test using the broker
// would need to know whether it already closed it.
func TestStartAndClose(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if broker.Addr() == "" {
		t.Fatal("the broker reported no address")
	}
	if _, err := net.DialTimeout("tcp", broker.Addr(), testTimeout); err != nil {
		t.Fatalf("the broker is not accepting connections: %v", err)
	}

	if err := broker.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := broker.Close(); err != nil {
		t.Fatalf("a second close returned %v, want nil", err)
	}
}

// TestPublishRoundTrip verifies routing between two clients, which is the
// property every integration test built on this broker depends on.
func TestPublishRoundTrip(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	delivered := make(chan string, 8)
	subscriber, err := mqtt.New(mqtt.Config{
		Address:       broker.Addr(),
		ClientID:      "subscriber",
		Logger:        quietLogger(),
		ReconnectMin:  20 * time.Millisecond,
		Subscriptions: []mqtt.TopicFilter{{Topic: "device/telemetry", QoS: 1}},
		Handler: func(_ context.Context, message mqtt.Message) {
			select {
			case delivered <- string(message.Payload):
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("new subscriber: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = subscriber.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitFor(t, "the subscriber to connect", subscriber.Connected)

	publisher := dial(t, broker, "publisher")
	waitFor(t, "the publisher to connect", publisher.Connected)

	for _, payload := range []string{`{"n":1}`, `{"n":2}`} {
		if err := publisher.Publish(context.Background(), "device/telemetry", []byte(payload), 1); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	for index := 0; index < 2; index++ {
		select {
		case <-delivered:
		case <-time.After(testTimeout):
			t.Fatalf("only %d of 2 messages were delivered", index)
		}
	}

	// The broker records what it routed, so a test can assert on the traffic
	// rather than only on what one subscriber happened to see.
	if observed := broker.ObservedOn("device/telemetry"); len(observed) != 2 {
		t.Fatalf("the broker recorded %d messages on the topic", len(observed))
	}
	if observed := broker.Observed(); len(observed) < 2 {
		t.Fatalf("the broker recorded %d messages in total", len(observed))
	}
	observed := broker.ObservedOn("device/telemetry")[0]
	if observed.Client != "publisher" {
		t.Fatalf("the recorded publisher is %q", observed.Client)
	}
	// The copy must be independent, so a later message cannot rewrite it.
	if string(observed.Payload) != `{"n":1}` {
		t.Fatalf("the recorded payload is %s", observed.Payload)
	}
}

// TestQoS0IsNotAcknowledged verifies that a QoS 0 publish still routes; the
// broker must not require the handshake to deliver.
func TestQoS0IsNotAcknowledged(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	publisher := dial(t, broker, "publisher")
	waitFor(t, "the publisher to connect", publisher.Connected)

	if err := publisher.Publish(context.Background(), "device/telemetry", []byte("fire and forget"), 0); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitFor(t, "the broker to record the message", func() bool {
		return len(broker.ObservedOn("device/telemetry")) == 1
	})
}

// TestCredentialsAreEnforced verifies the authentication branch a deployment
// relies on when it configures a broker account.
func TestCredentialsAreEnforced(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	broker.Credentials = &mqtttest.Credentials{Username: "backend", Password: "right"}

	wrong, err := mqtt.New(mqtt.Config{
		Address:  broker.Addr(),
		ClientID: "wrong",
		Username: "backend",
		Password: "wrong",
		Logger:   quietLogger(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = wrong.Run(ctx)
	}()
	time.Sleep(200 * time.Millisecond)
	if wrong.Connected() {
		t.Fatal("the broker accepted a wrong password")
	}
	cancel()
	<-done

	right, err := mqtt.New(mqtt.Config{
		Address:  broker.Addr(),
		ClientID: "right",
		Username: "backend",
		Password: "right",
		Logger:   quietLogger(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	rightCtx, rightCancel := context.WithCancel(context.Background())
	rightDone := make(chan struct{})
	go func() {
		defer close(rightDone)
		_ = right.Run(rightCtx)
	}()
	t.Cleanup(func() {
		rightCancel()
		<-rightDone
	})
	waitFor(t, "the correctly credentialled client to connect", right.Connected)
}

// TestRejectSubscriptions verifies the branch a test uses to simulate a broker
// that will not grant a subscription.
func TestRejectSubscriptions(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	broker.RejectSubscriptions = true

	client := dial(t, broker, "client", mqtt.TopicFilter{Topic: "device/telemetry", QoS: 1})
	time.Sleep(200 * time.Millisecond)
	if client.Connected() {
		t.Fatal("the client stayed connected although its subscription was refused")
	}
}

// TestDropConnectionsPreservesTheListener verifies that the chaos hook used to
// simulate a broker restart closes the clients but keeps accepting new ones,
// which is what makes a reconnect test meaningful.
func TestDropConnectionsPreservesTheListener(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	client := dial(t, broker, "client")
	waitFor(t, "the client to connect", client.Connected)

	broker.DropConnections()
	waitFor(t, "the client to reconnect", client.Connected)

	// A fresh client can still connect.
	other := dial(t, broker, "other")
	waitFor(t, "a new client to connect", other.Connected)
}

// TestUnsupportedPacketClosesTheConnection verifies that a peer speaking
// something the broker does not implement is disconnected rather than ignored,
// so an integration test cannot pass against a half-read stream.
func TestUnsupportedPacketClosesTheConnection(t *testing.T) {
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })

	conn, err := net.DialTimeout("tcp", broker.Addr(), testTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// A CONNECT the broker accepts, followed by a PUBREC packet it does not
	// implement.
	connect := &mqtt.Packet{Type: mqtt.PacketCONNECT, ClientID: "raw", KeepAlive: 30, CleanSession: true}
	if err := connect.Encode(conn); err != nil {
		t.Fatalf("send CONNECT: %v", err)
	}
	if _, err := mqtt.ReadPacket(conn, 0); err != nil {
		t.Fatalf("read CONNACK: %v", err)
	}

	unsupported := &mqtt.Packet{Type: mqtt.PacketPUBREC, PacketID: 1}
	if err := unsupported.Encode(conn); err != nil {
		t.Fatalf("send PUBREC: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(testTimeout))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("the broker kept the connection open after an unsupported packet")
	}
}

// TestMatchTopicWildcards covers the matcher the routing relies on.
func TestMatchTopicWildcards(t *testing.T) {
	cases := []struct {
		filter string
		topic  string
		want   bool
	}{
		{"device/telemetry", "device/telemetry", true},
		{"device/telemetry", "device/control", false},
		{"device/#", "device/telemetry", true},
		{"device/#", "device", true},
		{"device/#", "other/telemetry", false},
		{"device/+", "device/telemetry", true},
		{"device/+", "device/a/b", false},
		{"device", "device/telemetry", false},
		{"device/telemetry/raw", "device/telemetry", false},
		{"#", "anything", true},
	}
	for _, testCase := range cases {
		if got := mqtttest.MatchTopic(testCase.filter, testCase.topic); got != testCase.want {
			t.Errorf("MatchTopic(%q, %q) = %v, want %v", testCase.filter, testCase.topic, got, testCase.want)
		}
	}
}
