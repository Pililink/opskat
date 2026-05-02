package kafka_svc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"go.uber.org/mock/gomock"

	"github.com/opskat/opskat/internal/connpool"
	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	"github.com/opskat/opskat/internal/repository/asset_repo"
	"github.com/opskat/opskat/internal/repository/asset_repo/mock_asset_repo"
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

func TestKafkaLiveProduceAndBrowse(t *testing.T) {
	brokersText := strings.TrimSpace(os.Getenv("OPSKAT_KAFKA_TEST_BROKERS"))
	if brokersText == "" {
		t.Skip("set OPSKAT_KAFKA_TEST_BROKERS to run live Kafka message test")
	}

	ctx := context.Background()
	cfg := &asset_entity.KafkaConfig{
		Brokers:               splitBrokers(brokersText),
		SASLMechanism:         asset_entity.KafkaSASLNone,
		RequestTimeoutSeconds: 5,
		MessageFetchLimit:     10,
		MessagePreviewBytes:   1024,
	}
	asset := &asset_entity.Asset{ID: 9001, Name: "live-kafka", Type: asset_entity.AssetTypeKafka}
	require.NoError(t, asset.SetKafkaConfig(cfg))

	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)
	mockRepo := mock_asset_repo.NewMockAssetRepo(mockCtrl)
	mockRepo.EXPECT().Find(gomock.Any(), int64(9001)).Return(asset, nil).AnyTimes()
	origRepo := asset_repo.Asset()
	asset_repo.RegisterAsset(mockRepo)
	t.Cleanup(func() {
		if origRepo != nil {
			asset_repo.RegisterAsset(origRepo)
		}
	})

	adminClient, err := connpool.DialKafka(ctx, asset, cfg, "", nil)
	require.NoError(t, err)
	defer adminClient.Close()
	admin := kadm.NewClient(adminClient)
	topic := fmt.Sprintf("opskat-live-%d", time.Now().UnixNano())
	created, err := admin.CreateTopic(ctx, 1, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, created.Err)
	t.Cleanup(func() { _, _ = admin.DeleteTopic(context.Background(), topic) })

	svc := New(nil)
	defer svc.Close()

	partition := int32(0)
	produced, err := svc.ProduceMessage(ctx, ProduceMessageRequest{
		AssetID:   asset.ID,
		Topic:     topic,
		Partition: &partition,
		Key:       "opskat-live-key",
		Value:     "opskat-live-value",
		Headers: []ProduceMessageHeader{
			{Key: "source", Value: "opskat-live"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, topic, produced.Topic)
	assert.Equal(t, partition, produced.Partition)

	browsed, err := svc.BrowseMessages(ctx, BrowseMessagesRequest{
		AssetID:       asset.ID,
		Topic:         topic,
		Partition:     &partition,
		StartMode:     "oldest",
		Limit:         5,
		MaxBytes:      1024,
		DecodeMode:    "text",
		MaxWaitMillis: 5000,
	})
	require.NoError(t, err)
	require.NotEmpty(t, browsed.Records)
	assert.Equal(t, "opskat-live-key", browsed.Records[0].Key)
	assert.Equal(t, "opskat-live-value", browsed.Records[0].Value)
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
