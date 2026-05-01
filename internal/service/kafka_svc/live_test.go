package kafka_svc

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/opskat/opskat/internal/model/entity/asset_entity"
)

func TestKafkaLiveConnection(t *testing.T) {
	brokersText := strings.TrimSpace(os.Getenv("OPSKAT_KAFKA_TEST_BROKERS"))
	if brokersText == "" {
		t.Skip("set OPSKAT_KAFKA_TEST_BROKERS to run live Kafka connection test")
	}

	cfg := &asset_entity.KafkaConfig{
		Brokers:               splitBrokers(brokersText),
		SASLMechanism:         asset_entity.KafkaSASLNone,
		RequestTimeoutSeconds: 5,
	}
	svc := New(nil)
	defer svc.Close()

	if err := svc.TestConnection(context.Background(), cfg, "", 0); err != nil {
		t.Fatalf("test kafka connection: %v", err)
	}
}

func splitBrokers(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
