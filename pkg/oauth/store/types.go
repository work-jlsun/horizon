package store

import (
	"g.hz.netease.com/horizon/pkg/oauth/models"
	"golang.org/x/net/context"
)

type TokenStore interface {
	Create(ctx context.Context, token *models.Token) error
	DeleteByCode(ctx context.Context, code string) error
	DeleteByClientID(ctx context.Context, code string) error
	Get(ctx context.Context, code string) (*models.Token, error)
}

type OauthAppStore interface {
	CreateApp(ctx context.Context, client models.OauthApp) error
	GetApp(ctx context.Context, clientID string) (*models.OauthApp, error)
	DeleteApp(ctx context.Context, clientID string) error
	CreateSecret(ctx context.Context, secret *models.ClientSecret) (*models.ClientSecret, error)
	DeleteSecret(ctx context.Context, clientID string, clientSecretID uint) error
	DeleteSecretByClientID(ctx context.Context, clientID string) error
	ListSecret(ctx context.Context, clientID string) ([]models.ClientSecret, error)
}
