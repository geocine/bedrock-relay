package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type BedrockInvoker interface {
	Invoke(ctx context.Context, modelID string, body []byte) ([]byte, error)
	Stream(ctx context.Context, modelID string, body []byte) (<-chan []byte, <-chan error, error)
}

type AWSBedrockInvoker struct {
	client *bedrockruntime.Client
}

func NewAWSBedrockInvoker(client *bedrockruntime.Client) AWSBedrockInvoker {
	return AWSBedrockInvoker{client: client}
}

func (b AWSBedrockInvoker) Invoke(ctx context.Context, modelID string, body []byte) ([]byte, error) {
	out, err := b.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, err
	}
	return io.ReadAll(bytes.NewReader(out.Body))
}

func (b AWSBedrockInvoker) Stream(ctx context.Context, modelID string, body []byte) (<-chan []byte, <-chan error, error) {
	out, err := b.client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, nil, err
	}

	chunks := make(chan []byte)
	errs := make(chan error, 1)
	stream := out.GetStream()
	go func() {
		defer close(chunks)
		defer close(errs)
		defer stream.Close()

		for event := range stream.Events() {
			switch v := event.(type) {
			case *brtypes.ResponseStreamMemberChunk:
				chunks <- append([]byte(nil), v.Value.Bytes...)
			default:
				errs <- fmt.Errorf("unexpected Bedrock stream event %T", event)
				return
			}
		}
		if err := stream.Err(); err != nil {
			errs <- err
		}
	}()
	return chunks, errs, nil
}
