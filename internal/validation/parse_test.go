package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgs_Sigstore(t *testing.T) {
	args := []string{
		"verify", "sigstore",
		"--signature=/path/to/sig",
		"--identity", "test@example.com",
		"--identity_provider", "https://accounts.google.com",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, "sigstore", cfg.Method)
	assert.Equal(t, "/path/to/model", cfg.ModelPath)
	assert.Equal(t, "/path/to/sig", cfg.SignaturePath)
	assert.Equal(t, "test@example.com", cfg.Identity)
	assert.Equal(t, "https://accounts.google.com", cfg.IdentityProvider)
}

func TestParseArgs_Key(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, "key", cfg.Method)
	assert.Equal(t, "/path/to/model", cfg.ModelPath)
	assert.Equal(t, "/path/to/sig", cfg.SignaturePath)
	assert.Equal(t, "/path/to/key.pub", cfg.PublicKeyPath)
}

func TestParseArgs_Certificate(t *testing.T) {
	args := []string{
		"verify", "certificate",
		"--signature=/path/to/sig",
		"--certificate_chain", "/path/to/ca.pem",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, "certificate", cfg.Method)
	assert.Equal(t, "/path/to/model", cfg.ModelPath)
	assert.Equal(t, "/path/to/sig", cfg.SignaturePath)
	assert.Equal(t, "/path/to/ca.pem", cfg.CertificateChainPath)
}

func TestParseArgs_WithTrustConfig(t *testing.T) {
	args := []string{
		"verify", "sigstore",
		"--signature=/path/to/sig",
		"--identity", "test@example.com",
		"--identity_provider", "https://accounts.google.com",
		"--trust_config", "/path/to/trust.json",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, "/path/to/trust.json", cfg.TrustConfigPath)
}

func TestParseArgs_WithIgnorePaths(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
		"--ignore-paths", "/tmp",
		"--ignore-paths", "/cache",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp", "/cache"}, cfg.IgnorePaths)
}

func TestParseArgs_WithBooleanFlags(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
		"--ignore-git-paths",
		"--no-ignore_unsigned_files",
		"--allow_symlinks",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	require.NotNil(t, cfg.IgnoreGitPaths)
	assert.True(t, *cfg.IgnoreGitPaths)
	require.NotNil(t, cfg.IgnoreUnsignedFiles)
	assert.False(t, *cfg.IgnoreUnsignedFiles)
	require.NotNil(t, cfg.AllowSymlinks)
	assert.True(t, *cfg.AllowSymlinks)
}

func TestParseArgs_NegatedBooleanFlags(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
		"--no-ignore-git-paths",
		"--ignore_unsigned_files",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	require.NotNil(t, cfg.IgnoreGitPaths)
	assert.False(t, *cfg.IgnoreGitPaths)
	require.NotNil(t, cfg.IgnoreUnsignedFiles)
	assert.True(t, *cfg.IgnoreUnsignedFiles)
	assert.Nil(t, cfg.AllowSymlinks)
}

func TestParseArgs_NilBooleans(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
		"/path/to/model",
	}

	cfg, err := ParseArgs(args)
	require.NoError(t, err)
	assert.Nil(t, cfg.IgnoreGitPaths)
	assert.Nil(t, cfg.IgnoreUnsignedFiles)
	assert.Nil(t, cfg.AllowSymlinks)
}

func TestParseArgs_TooFewArgs(t *testing.T) {
	_, err := ParseArgs([]string{"verify", "sigstore"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected at least 3 arguments")
}

func TestParseArgs_InvalidVerb(t *testing.T) {
	_, err := ParseArgs([]string{"sign", "sigstore", "/model"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected first argument to be 'verify'")
}

func TestParseArgs_InvalidMethod(t *testing.T) {
	_, err := ParseArgs([]string{"verify", "unknown", "/model"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verification method")
}

func TestParseArgs_MissingModelPath(t *testing.T) {
	args := []string{
		"verify", "key",
		"--signature=/path/to/sig",
		"--public_key", "/path/to/key.pub",
	}
	_, err := ParseArgs(args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model path is required")
}

func TestParseArgs_MissingFlagValue(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "missing identity value",
			args: []string{"verify", "sigstore", "--signature=/sig", "--identity"},
		},
		{
			name: "missing identity_provider value",
			args: []string{"verify", "sigstore", "--signature=/sig", "--identity_provider"},
		},
		{
			name: "missing public_key value",
			args: []string{"verify", "key", "--signature=/sig", "--public_key"},
		},
		{
			name: "missing certificate_chain value",
			args: []string{"verify", "certificate", "--signature=/sig", "--certificate_chain"},
		},
		{
			name: "missing trust_config value",
			args: []string{"verify", "sigstore", "--signature=/sig", "--trust_config"},
		},
		{
			name: "missing ignore-paths value",
			args: []string{"verify", "key", "--signature=/sig", "--ignore-paths"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseArgs(tc.args)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "requires a value")
		})
	}
}
