package relay

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

const (
	defaultPort      = "18456"
	modelConfigPath  = "models.json"
	anthropicVersion = "bedrock-2023-05-31"
	defaultMaxTokens = 4096
)

type AppConfig struct {
	Port       string
	AWSProfile string
	Region     string
	Models     ModelCatalog
}

// LoadAppConfig parses command-line and environment configuration, then creates a Bedrock client.
func LoadAppConfig(ctx context.Context, args []string) (AppConfig, *bedrockruntime.Client, error) {
	fs := flag.NewFlagSet("bedrock-relay", flag.ContinueOnError)
	port := fs.String("port", defaultPort, "listen port")
	if err := fs.Parse(args); err != nil {
		return AppConfig{}, nil, err
	}

	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		return AppConfig{}, nil, errors.New("AWS_PROFILE is required")
	}

	models, err := LoadModelCatalog(modelConfigPath)
	if err != nil {
		return AppConfig{}, nil, err
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))
	if err != nil {
		return AppConfig{}, nil, fmt.Errorf("load AWS config: %w", err)
	}
	if awsCfg.Region == "" {
		return AppConfig{}, nil, fmt.Errorf("AWS profile %q does not define a region", profile)
	}

	return AppConfig{
		Port:       *port,
		AWSProfile: profile,
		Region:     awsCfg.Region,
		Models:     models,
	}, bedrockruntime.NewFromConfig(awsCfg), nil
}
