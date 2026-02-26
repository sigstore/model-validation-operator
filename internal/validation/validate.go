// Package validation wraps the model-transparency-go library to provide
// model verification using Sigstore, public key, or certificate methods.
package validation

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/sigstore/model-signing/pkg/logging"
	"github.com/sigstore/model-signing/pkg/verify/certificate"
	"github.com/sigstore/model-signing/pkg/verify/key"
	"github.com/sigstore/model-signing/pkg/verify/sigstore"
	"k8s.io/utils/ptr"
)

// Config holds all parameters needed for model validation,
// populated from CLI args or directly from the webhook.
type Config struct {
	Method string // "sigstore", "key", or "certificate"

	ModelPath     string
	SignaturePath string

	// Sigstore-specific
	Identity         string
	IdentityProvider string

	// Key-specific
	PublicKeyPath string

	// Certificate-specific
	CertificateChainPath string

	// Common options
	TrustConfigPath     string
	IgnorePaths         []string
	IgnoreGitPaths      *bool
	IgnoreUnsignedFiles *bool
	AllowSymlinks       *bool
}

// Validate runs model verification using the appropriate verifier.
func Validate(ctx context.Context, cfg Config, logger logr.Logger) error {
	libLogger := &logrAdapter{logger: logger}

	switch cfg.Method {
	case "sigstore":
		return validateSigstore(ctx, cfg, libLogger)
	case "key":
		return validateKey(ctx, cfg, libLogger)
	case "certificate":
		return validateCertificate(ctx, cfg, libLogger)
	default:
		return fmt.Errorf("unknown verification method: %q", cfg.Method)
	}
}

func validateSigstore(ctx context.Context, cfg Config, logger logging.Logger) error {
	opts := sigstore.SigstoreVerifierOptions{
		ModelPath:           cfg.ModelPath,
		SignaturePath:       cfg.SignaturePath,
		Identity:            cfg.Identity,
		IdentityProvider:    cfg.IdentityProvider,
		TrustConfigPath:     cfg.TrustConfigPath,
		IgnorePaths:         cfg.IgnorePaths,
		IgnoreGitPaths:      ptr.Deref(cfg.IgnoreGitPaths, false),
		IgnoreUnsignedFiles: ptr.Deref(cfg.IgnoreUnsignedFiles, false),
		AllowSymlinks:       ptr.Deref(cfg.AllowSymlinks, false),
		Logger:              logger,
	}

	verifier, err := sigstore.NewSigstoreVerifier(opts)
	if err != nil {
		return fmt.Errorf("failed to create sigstore verifier: %w", err)
	}

	result, err := verifier.Verify(ctx)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	if !result.Verified {
		return fmt.Errorf("verification failed: %s", result.Message)
	}

	logger.Infoln(result.Message)
	return nil
}

func validateKey(ctx context.Context, cfg Config, logger logging.Logger) error {
	opts := key.KeyVerifierOptions{
		ModelPath:           cfg.ModelPath,
		SignaturePath:       cfg.SignaturePath,
		PublicKeyPath:       cfg.PublicKeyPath,
		IgnorePaths:         cfg.IgnorePaths,
		IgnoreGitPaths:      ptr.Deref(cfg.IgnoreGitPaths, false),
		IgnoreUnsignedFiles: ptr.Deref(cfg.IgnoreUnsignedFiles, false),
		AllowSymlinks:       ptr.Deref(cfg.AllowSymlinks, false),
		Logger:              logger,
	}

	verifier, err := key.NewKeyVerifier(opts)
	if err != nil {
		return fmt.Errorf("failed to create key verifier: %w", err)
	}

	result, err := verifier.Verify(ctx)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	if !result.Verified {
		return fmt.Errorf("verification failed: %s", result.Message)
	}

	logger.Infoln(result.Message)
	return nil
}

func validateCertificate(ctx context.Context, cfg Config, logger logging.Logger) error {
	var certChain []string
	if cfg.CertificateChainPath != "" {
		certChain = []string{cfg.CertificateChainPath}
	}

	opts := certificate.CertificateVerifierOptions{
		ModelPath:           cfg.ModelPath,
		SignaturePath:       cfg.SignaturePath,
		CertificateChain:    certChain,
		IgnorePaths:         cfg.IgnorePaths,
		IgnoreGitPaths:      ptr.Deref(cfg.IgnoreGitPaths, false),
		IgnoreUnsignedFiles: ptr.Deref(cfg.IgnoreUnsignedFiles, false),
		AllowSymlinks:       ptr.Deref(cfg.AllowSymlinks, false),
		Logger:              logger,
	}

	verifier, err := certificate.NewCertificateVerifier(opts)
	if err != nil {
		return fmt.Errorf("failed to create certificate verifier: %w", err)
	}

	result, err := verifier.Verify(ctx)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	if !result.Verified {
		return fmt.Errorf("verification failed: %s", result.Message)
	}

	logger.Infoln(result.Message)
	return nil
}

// logrAdapter adapts logr.Logger to the logging.Logger interface
// used by the model-transparency-go library.
type logrAdapter struct {
	logger logr.Logger
	fields map[string]interface{}
}

func (a *logrAdapter) Debug(format string, args ...interface{}) {
	a.logger.V(1).Info(fmt.Sprintf(format, args...))
}

func (a *logrAdapter) Debugln(msg string) {
	a.logger.V(1).Info(msg)
}

func (a *logrAdapter) Info(format string, args ...interface{}) {
	a.logger.Info(fmt.Sprintf(format, args...))
}

func (a *logrAdapter) Infoln(msg string) {
	a.logger.Info(msg)
}

func (a *logrAdapter) Warn(format string, args ...interface{}) {
	a.logger.Info(fmt.Sprintf("[WARN] "+format, args...))
}

func (a *logrAdapter) Warnln(msg string) {
	a.logger.Info("[WARN] " + msg)
}

func (a *logrAdapter) Error(format string, args ...interface{}) {
	a.logger.Error(nil, fmt.Sprintf(format, args...))
}

func (a *logrAdapter) Errorln(msg string) {
	a.logger.Error(nil, msg)
}

func (a *logrAdapter) GetLevel() logging.LogLevel {
	if a.logger.V(1).Enabled() {
		return logging.LevelDebug
	}
	return logging.LevelInfo
}

func (a *logrAdapter) Silent() bool {
	return false
}

func (a *logrAdapter) WithField(k string, value interface{}) logging.Logger {
	newFields := make(map[string]interface{}, len(a.fields)+1)
	for fk, fv := range a.fields {
		newFields[fk] = fv
	}
	newFields[k] = value
	return &logrAdapter{
		logger: a.logger.WithValues(k, value),
		fields: newFields,
	}
}

func (a *logrAdapter) WithFields(fields map[string]interface{}) logging.Logger {
	newFields := make(map[string]interface{}, len(a.fields)+len(fields))
	for fk, fv := range a.fields {
		newFields[fk] = fv
	}
	kvs := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		newFields[k] = v
		kvs = append(kvs, k, v)
	}
	return &logrAdapter{
		logger: a.logger.WithValues(kvs...),
		fields: newFields,
	}
}
