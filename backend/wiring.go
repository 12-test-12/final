package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/BobcGn/final/backend/internal/config"
	"github.com/BobcGn/final/backend/internal/ingest"
	"github.com/BobcGn/final/backend/internal/mqtt"
	"github.com/BobcGn/final/backend/internal/protocol"
)

// errNoBroker reports a publish attempted while no broker client exists. It is
// the honest answer when the deployment runs without MQTT: the command cannot
// reach a device, and the API must surface that as a broker failure rather than
// record a command the device will never see.
var errNoBroker = errors.New("mqtt: no broker client is configured")

// brokerPublisher adapts the MQTT client to the command service's Publisher
// port.
//
// The indirection exists because the broker client is constructed after the
// command service: the client's inbound handler is the ingest service, which
// needs the alert engine and the tracker, and building the client first would
// mean building those twice. The holder is written once during start-up and read
// afterwards, guarded so the race detector stays honest.
type brokerPublisher struct {
	mu     sync.RWMutex
	client *mqtt.Client
}

// set installs the client. It is called once during start-up.
func (p *brokerPublisher) set(client *mqtt.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.client = client
}

// Publish implements command.Publisher.
func (p *brokerPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte) error {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	if client == nil {
		return errNoBroker
	}
	return client.Publish(ctx, topic, payload, qos)
}

// newBrokerClient builds the MQTT client and points its inbound handler at the
// ingest service.
//
// The subscription list is exactly the two device-to-cloud topics of the frozen
// contract. The backend never subscribes to the control topic: it publishes
// there, and subscribing to its own commands would make it consume the messages
// it is responsible for delivering.
func newBrokerClient(cfg config.Config, handler *ingest.Service, logger *slog.Logger) (*mqtt.Client, error) {
	address := cfg.BrokerURL
	// Accept a URL-shaped value so an operator can paste what their broker admin
	// documented, but only strip a scheme the deployment actually supports.
	for _, scheme := range []string{"tcp://", "mqtt://", "ssl://", "tls://", "mqtts://"} {
		if len(address) > len(scheme) && address[:len(scheme)] == scheme {
			address = address[len(scheme):]
			break
		}
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return nil, fmt.Errorf("backend: MQTT_BROKER_URL %q must be host:port", cfg.BrokerURL)
	}

	clientCfg := mqtt.Config{
		Address:      address,
		ClientID:     cfg.MQTTClientID,
		Username:     cfg.BrokerUser,
		Password:     cfg.BrokerPass,
		KeepAlive:    cfg.MQTTKeepAlive,
		CleanSession: true,
		Logger:       logger,
		Subscriptions: []mqtt.TopicFilter{
			{Topic: protocol.TopicTelemetry, QoS: 1},
			{Topic: protocol.TopicCommandAck, QoS: 1},
		},
		Handler: func(ctx context.Context, message mqtt.Message) {
			if err := handler.HandleMessage(ctx, message); err != nil {
				// A single rejected message must not tear down the session: the
				// counters in the ingest service are what an operator watches for
				// a systematic problem.
				logger.LogAttrs(ctx, slog.LevelWarn, "message rejected",
					slog.String("topic", message.Topic),
					slog.String("error", err.Error()))
			}
		},
	}
	if cfg.BrokerTLS {
		clientCfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return mqtt.New(clientCfg)
}
