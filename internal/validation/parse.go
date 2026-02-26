package validation

import (
	"fmt"
	"strings"

	"k8s.io/utils/ptr"
)

// ParseArgs parses CLI-style validation arguments into a Config.
// Expected format: verify <method> [flags...] <model-path>
func ParseArgs(args []string) (Config, error) {
	if len(args) < 3 {
		return Config{}, fmt.Errorf("expected at least 3 arguments (verify <method> <model-path>), got %d", len(args))
	}

	if args[0] != "verify" {
		return Config{}, fmt.Errorf("expected first argument to be 'verify', got %q", args[0])
	}

	method := args[1]
	switch method {
	case "sigstore", "key", "certificate":
		// valid
	default:
		return Config{}, fmt.Errorf("unknown verification method %q, expected sigstore, key, or certificate", method)
	}

	cfg := Config{
		Method: method,
	}

	// Parse flags and positional args (model path is the last non-flag argument)
	i := 2
	for i < len(args) {
		arg := args[i]

		switch {
		case strings.HasPrefix(arg, "--signature="):
			cfg.SignaturePath = strings.TrimPrefix(arg, "--signature=")
			i++
		case arg == "--identity":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--identity requires a value")
			}
			cfg.Identity = args[i+1]
			i += 2
		case arg == "--identity_provider":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--identity_provider requires a value")
			}
			cfg.IdentityProvider = args[i+1]
			i += 2
		case arg == "--public_key":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--public_key requires a value")
			}
			cfg.PublicKeyPath = args[i+1]
			i += 2
		case arg == "--certificate_chain":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--certificate_chain requires a value")
			}
			cfg.CertificateChainPath = args[i+1]
			i += 2
		case arg == "--trust_config":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--trust_config requires a value")
			}
			cfg.TrustConfigPath = args[i+1]
			i += 2
		case arg == "--ignore-paths":
			if i+1 >= len(args) {
				return Config{}, fmt.Errorf("--ignore-paths requires a value")
			}
			cfg.IgnorePaths = append(cfg.IgnorePaths, args[i+1])
			i += 2
		case arg == "--ignore-git-paths":
			cfg.IgnoreGitPaths = ptr.To(true)
			i++
		case arg == "--no-ignore-git-paths":
			cfg.IgnoreGitPaths = ptr.To(false)
			i++
		case arg == "--ignore_unsigned_files":
			cfg.IgnoreUnsignedFiles = ptr.To(true)
			i++
		case arg == "--no-ignore_unsigned_files":
			cfg.IgnoreUnsignedFiles = ptr.To(false)
			i++
		case arg == "--allow_symlinks":
			cfg.AllowSymlinks = ptr.To(true)
			i++
		default:
			// Must be the model path (last positional argument)
			if i != len(args)-1 {
				return Config{}, fmt.Errorf("unexpected argument %q at position %d", arg, i)
			}
			cfg.ModelPath = arg
			i++
		}
	}

	if cfg.ModelPath == "" {
		return Config{}, fmt.Errorf("model path is required as the last argument")
	}

	return cfg, nil
}
