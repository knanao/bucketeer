// Copyright 2022 The Bucketeer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package datastore

import (
	"context"
	"time"

	"github.com/Shopify/sarama"
	"go.uber.org/zap"

	"github.com/bucketeer-io/bucketeer/pkg/metrics"
	storagekafka "github.com/bucketeer-io/bucketeer/pkg/storage/kafka"
)

type options struct {
	metrics metrics.Registerer
	logger  *zap.Logger
}

type Option func(*options)

func WithMetrics(r metrics.Registerer) Option {
	return func(opts *options) {
		opts.metrics = r
	}
}

func WithLogger(l *zap.Logger) Option {
	return func(opts *options) {
		opts.logger = l
	}
}

type kafkaWriter struct {
	producer      *storagekafka.Producer
	topicPrefix   string
	topicDataType string
	logger        *zap.Logger
}

func NewKafkaWriter(
	producer *storagekafka.Producer,
	topicPrefix,
	topicDataType string,
	opts ...Option,
) (Writer, error) {
	dopts := &options{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(dopts)
	}
	if dopts.metrics != nil {
		registerMetrics(dopts.metrics)
	}
	return &kafkaWriter{
		producer:      producer,
		topicPrefix:   topicPrefix,
		topicDataType: topicDataType,
		logger:        dopts.logger.Named("kafka"),
	}, nil
}

func (w *kafkaWriter) Close() {
	if err := w.producer.Close(); err != nil {
		w.logger.Error("Close failed", zap.Error(err))
	}
}

func (w *kafkaWriter) Write(
	ctx context.Context,
	events map[string]string,
	environmentNamespace string,
) (fails map[string]bool, err error) {
	startTime := time.Now()
	defer func() {
		code := codeSuccess
		if err != nil {
			code = codeFail
		}
		writeCounter.WithLabelValues(writerKafka, code).Inc()
		wroteHistogram.WithLabelValues(writerKafka, code).Observe(time.Since(startTime).Seconds())
	}()
	messages := make([]*sarama.ProducerMessage, 0, len(events))
	for id, event := range events {
		messages = append(messages, &sarama.ProducerMessage{
			Key:   sarama.StringEncoder(id),
			Topic: storagekafka.TopicName(w.topicPrefix, w.topicDataType),
			Value: sarama.StringEncoder(event),
		})
	}
	err = w.producer.SendMessages(messages)
	merr, ok := err.(sarama.ProducerErrors)
	if !ok {
		return nil, err
	}
	fails = make(map[string]bool, len(events))
	for _, e := range merr {
		id := string(e.Msg.Key.(sarama.StringEncoder))
		if !w.isRepeatable(e.Err) {
			fails[id] = false
			w.logger.Error("MultiError NonRepeatable",
				zap.Error(e),
				zap.String("environmentNamespace", environmentNamespace),
				zap.Any("msg", id),
			)
			break
		}
		w.logger.Error("MultiError Repeatable",
			zap.Error(e),
			zap.String("environmentNamespace", environmentNamespace),
			zap.Any("msg", id),
		)
	}
	return fails, nil
}

func (w *kafkaWriter) isRepeatable(err error) bool {
	switch err {
	case sarama.ErrInvalidMessage:
		return false
	}
	return true
}
