package config

import "testing"

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSelectDefaultModel(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"glm key wins", map[string]string{"GLM_API_KEY": "x"}, defaultProviderGLM},
		{"glm beats deepseek", map[string]string{"GLM_API_KEY": "x", "DEEPSEEK_API_KEY": "y"}, defaultProviderGLM},
		{"glm beats zai", map[string]string{"GLM_API_KEY": "x", "ZAI_API_KEY": "y"}, defaultProviderGLM},
		{"zai when no glm", map[string]string{"ZAI_API_KEY": "y"}, defaultProviderZAI},
		{"deepseek only is respected", map[string]string{"DEEPSEEK_API_KEY": "y"}, defaultProviderDeepSeek},
		{"no keys defaults to glm", map[string]string{}, defaultProviderGLM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectDefaultModel(envFrom(tc.env)); got != tc.want {
				t.Fatalf("selectDefaultModel(%v) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestApplyEnvDefaultProviderInjectsGLM(t *testing.T) {
	cfg := Default() // ships DeepSeek providers only
	if _, ok := cfg.Provider(defaultProviderGLM); ok {
		t.Fatal("precondition: Default() must not already contain a glm provider")
	}
	applyEnvDefaultProvider(cfg, envFrom(map[string]string{"GLM_API_KEY": "x"}))

	if cfg.DefaultModel != defaultProviderGLM {
		t.Fatalf("DefaultModel = %q, want %q", cfg.DefaultModel, defaultProviderGLM)
	}
	p, ok := cfg.Provider(defaultProviderGLM)
	if !ok {
		t.Fatalf("glm provider not injected: %+v", cfg.Providers)
	}
	if p.APIKeyEnv != "GLM_API_KEY" || p.Default != "glm-5.3-flash" {
		t.Fatalf("glm provider = %+v, want GLM_API_KEY / glm-5.3-flash", p)
	}
	// DeepSeek must remain so the user can switch back.
	if _, ok := cfg.Provider("deepseek-flash"); !ok {
		t.Fatal("deepseek-flash must remain available for switching")
	}
}

func TestApplyEnvDefaultProviderDeepSeekOnlyNotStranded(t *testing.T) {
	cfg := Default()
	applyEnvDefaultProvider(cfg, envFrom(map[string]string{"DEEPSEEK_API_KEY": "y"}))
	if cfg.DefaultModel != defaultProviderDeepSeek {
		t.Fatalf("DefaultModel = %q, want %q (existing DeepSeek key respected)", cfg.DefaultModel, defaultProviderDeepSeek)
	}
	if _, ok := cfg.Provider(defaultProviderDeepSeek); !ok {
		t.Fatalf("deepseek-flash provider must resolve: %+v", cfg.Providers)
	}
}
