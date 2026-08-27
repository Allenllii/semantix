package config

import "os"

// Issue B1 (GLM adaptation): a fresh install defaults to GLM, stays switchable
// to DeepSeek, and never strands a user who only holds one vendor's key.
//
// The shipped config template (semantix-agent.example.toml) already defaults to GLM,
// so packaged installs get GLM out of the box. This file covers the *no-config*
// path — a source build or a bare run with no semantix-agent.toml — where the choice
// must be inferred from which API-key env var is present instead of guessing a
// vendor the user cannot authenticate against.

// defaultProviderName is the built-in provider selected when no config file
// pins default_model. GLM is the out-of-the-box default; a single-vendor
// environment resolves to that vendor so an existing key is never stranded.
const (
	defaultProviderGLM      = "glm"
	defaultProviderZAI      = "zai"
	defaultProviderDeepSeek = "deepseek-flash"
)

// regularFileExists reports whether path exists and is a regular file. Used to
// detect a truly config-less install (no project semantix-agent.toml on disk).
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// selectDefaultModel picks the built-in default provider name from the API-key
// env vars visible to getenv, in precedence order:
//
//	GLM_API_KEY      → glm            (Zhipu GLM, bigmodel.cn)
//	ZAI_API_KEY      → zai            (Z.AI global GLM)
//	DEEPSEEK_API_KEY → deepseek-flash (respect an existing DeepSeek-only setup)
//	(none)           → glm            (GLM is the shipped default)
//
// getenv is injected so the choice is deterministic and unit-testable; callers
// pass a lookup that layers a project .env over the process environment.
func selectDefaultModel(getenv func(string) string) string {
	switch {
	case getenv("GLM_API_KEY") != "":
		return defaultProviderGLM
	case getenv("ZAI_API_KEY") != "":
		return defaultProviderZAI
	case getenv("DEEPSEEK_API_KEY") != "":
		return defaultProviderDeepSeek
	default:
		return defaultProviderGLM
	}
}

// builtinGLMProvider returns the built-in GLM (Zhipu, bigmodel.cn) entry, mirroring
// the glm-cn curated preset. The openai kind auto-detects GLM by host and gates
// thinking via thinking.type, so no extra reasoning knobs are needed here.
func builtinGLMProvider() ProviderEntry {
	return ProviderEntry{
		Name: defaultProviderGLM, Kind: "openai",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		Models:       []string{"glm-5.3-flash", "glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo"},
		VisionModels: []string{"glm-5.3-flash"}, // glm-5.3-flash is natively multimodal (text+image+video)
		Default:      "glm-5.2", APIKeyEnv: "GLM_API_KEY",
		ContextWindow: 1_000_000,
	}
}

// builtinZAIProvider returns the built-in Z.AI (global GLM) entry.
func builtinZAIProvider() ProviderEntry {
	p := builtinGLMProvider()
	p.Name = defaultProviderZAI
	p.BaseURL = "https://api.z.ai/api/paas/v4"
	p.APIKeyEnv = "ZAI_API_KEY"
	return p
}

// applyEnvDefaultProvider is the no-config fallback: when neither a user nor a
// project config was loaded and default_model was not pinned, it selects the
// default provider from env keys and makes sure that provider exists in the
// in-memory config so the choice resolves. It never mutates a config the user
// or project already configured — callers must gate it on "no toml source".
func applyEnvDefaultProvider(cfg *Config, getenv func(string) string) {
	if cfg == nil || getenv == nil {
		return
	}
	name := selectDefaultModel(getenv)
	cfg.DefaultModel = name
	if _, ok := cfg.Provider(name); ok {
		return
	}
	switch name {
	case defaultProviderGLM:
		cfg.Providers = append(cfg.Providers, builtinGLMProvider())
	case defaultProviderZAI:
		cfg.Providers = append(cfg.Providers, builtinZAIProvider())
	}
}
