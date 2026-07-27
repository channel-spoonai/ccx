package flows

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/channel-spoonai/ccx/internal/config"
	"github.com/channel-spoonai/ccx/internal/menu"
	"github.com/channel-spoonai/ccx/internal/providers"
)

// Add runs the new-provider flow: pick a template from the catalog (or
// manual entry), customize fields, persist.
func Add(loaded *config.Loaded) error {
	existing := loaded.Config.Profiles

	var items []menu.CatalogItem
	templates, err := config.LoadExample()
	if err != nil {
		fmt.Printf("  \x1B[33m⚠ failed to load catalog: %s\x1B[0m\n", err)
	}
	for i := range templates {
		t := templates[i]
		items = append(items, menu.CatalogItem{
			Label:       t.Name,
			Description: t.Description,
			Payload:     &t,
		})
	}
	items = append(items, menu.CatalogItem{
		Label:       "Other (manual entry)",
		Description: "Add a provider not in the catalog manually",
		Payload:     "manual",
		Pinned:      true,
	})

	picked, err := menu.SelectFromCatalog(items, "Add new provider", 10)
	if err != nil || picked == nil {
		return nil
	}

	var newProfile *config.Profile
	switch v := picked.(type) {
	case *config.Profile:
		newProfile, err = customizeTemplate(*v, existing)
	case string:
		if v == "manual" {
			newProfile, err = addManual(existing)
		}
	}
	if err != nil {
		return err
	}
	if newProfile == nil {
		return nil
	}

	next := append(existing, *newProfile)
	if err := config.Save(loaded.Path, config.Config{Profiles: next}); err != nil {
		fmt.Printf("\n  \x1B[31msave failed: %s\x1B[0m\n", err)
	} else {
		fmt.Printf("\n  \x1B[32m✓\x1B[0m %q saved\n", newProfile.Name)
		fmt.Printf("  \x1B[90m%s\x1B[0m\n", loaded.Path)
	}
	fmt.Println()
	_, _ = menu.PromptLine("Press Enter to continue", menu.PromptOptions{})
	return nil
}

func customizeTemplate(tpl config.Profile, existing []config.Profile) (*config.Profile, error) {
	existingNames := nameSet(existing)
	isLM := providers.IsLMStudio(tpl.Name)
	isOR := providers.IsOpenRouter(&tpl)
	isNV := providers.IsNVIDIA(&tpl)

	fmt.Println()
	fmt.Printf("  \x1B[36m[%s]\x1B[0m configuration\n", tpl.Name)
	fmt.Println("  \x1B[90mUse env:VAR_NAME instead of a literal value to read from an environment variable at runtime.\x1B[0m")
	fmt.Println()

	// 이름 — 중복이면 " (copy)" 제안
	suggested := tpl.Name
	if existingNames[strings.ToLower(suggested)] {
		suggested = tpl.Name + " (copy)"
	}
	name, err := promptUnique("Profile name", suggested, existingNames)
	if err != nil {
		return nil, err
	}
	tpl.Name = name

	if isLM {
		if tpl.BaseURL, err = menu.PromptLine("baseUrl (endpoint)", menu.PromptOptions{Default: tpl.BaseURL, Prefill: true, Required: true}); err != nil {
			return nil, err
		}
	}

	prior := findPriorAuth(tpl.BaseURL, existing)
	if tpl.AuthToken != "" || prior.authToken != "" {
		def := prior.authToken
		if def == "" {
			def = tpl.AuthToken
		}
		if tpl.AuthToken, err = menu.PromptLine("authToken (Bearer token)", menu.PromptOptions{Default: def, Prefill: true, Required: true}); err != nil {
			return nil, err
		}
	}
	if tpl.APIKey != "" || prior.apiKey != "" {
		def := prior.apiKey
		if def == "" {
			def = tpl.APIKey
		}
		if tpl.APIKey, err = menu.PromptLine("apiKey", menu.PromptOptions{Default: def, Prefill: true, Required: true}); err != nil {
			return nil, err
		}
	}

	switch {
	case isLM:
		configureLMStudioModels(&tpl)
	case isOR:
		configureOpenRouterModels(&tpl)
	case isNV:
		configureNVIDIAModels(&tpl)
	default:
		configureAnthropicModels(&tpl)
	}

	return &tpl, nil
}

func addManual(existing []config.Profile) (*config.Profile, error) {
	fmt.Println()
	fmt.Println("  \x1B[90mEnter fields one by one. Ctrl+C to cancel.\x1B[0m")
	fmt.Println("  \x1B[90mUse env:VAR_NAME instead of a literal value to read from an environment variable at runtime.\x1B[0m")

	existingNames := nameSet(existing)
	name, err := promptUnique("Profile name", "", existingNames)
	if err != nil {
		return nil, err
	}
	description, _ := menu.PromptLine("Description (optional)", menu.PromptOptions{})
	baseURL, err := menu.PromptLine("baseUrl (e.g. https://api.example.com/anthropic)", menu.PromptOptions{Required: true})
	if err != nil {
		return nil, err
	}

	authType, err := menu.PromptChoice("Auth method", []string{
		"authToken — Authorization: Bearer header (z.ai, Kimi, Ollama, etc.)",
		"apiKey — x-api-key header (DeepSeek, MiniMax, OpenRouter, etc.)",
	})
	if err != nil {
		return nil, err
	}
	prior := findPriorAuth(baseURL, existing)
	var authValue string
	var priorVal string
	if authType == 0 {
		priorVal = prior.authToken
	} else {
		priorVal = prior.apiKey
	}
	if priorVal != "" {
		authValue, err = menu.PromptLine("Auth value", menu.PromptOptions{Default: priorVal, Prefill: true, Required: true})
	} else {
		authValue, err = menu.PromptLine("Auth value", menu.PromptOptions{Required: true})
	}
	if err != nil {
		return nil, err
	}

	opus, _ := menu.PromptLine("Model opus (optional)", menu.PromptOptions{})
	sonnet, _ := menu.PromptLine("Model sonnet (optional)", menu.PromptOptions{Default: opus})
	sonnetDef := sonnet
	if sonnetDef == "" {
		sonnetDef = opus
	}
	haiku, _ := menu.PromptLine("Model haiku (optional)", menu.PromptOptions{Default: sonnetDef})

	profile := &config.Profile{Name: name, BaseURL: baseURL, Description: description}
	if authType == 0 {
		profile.AuthToken = authValue
	} else {
		profile.APIKey = authValue
	}
	if m := buildModels(opus, sonnet, haiku); m != nil {
		profile.Models = m
	}
	return profile, nil
}

// Edit mutates the profile at index in-place and persists.
func Edit(loaded *config.Loaded, index int) error {
	existing := loaded.Config.Profiles
	if index < 0 || index >= len(existing) {
		return nil
	}
	original := existing[index]
	edited := original

	other := map[string]bool{}
	for i, p := range existing {
		if i != index {
			other[strings.ToLower(p.Name)] = true
		}
	}
	isLM := providers.IsLMStudio(original.Name)
	isOR := providers.IsOpenRouter(&original) || providers.IsOpenRouter(&edited)
	isNV := providers.IsNVIDIA(&original) || providers.IsNVIDIA(&edited)

	menu.ClearScreen()
	fmt.Println()
	fmt.Printf("  \x1B[1m\x1B[36m ccx \x1B[0m\x1B[90m— Edit profile: %s\x1B[0m\n", original.Name)
	fmt.Println("  \x1B[90mPress Enter to keep the current value, Ctrl+U to clear and re-enter\x1B[0m")
	fmt.Println("  \x1B[90mUse env:VAR_NAME instead of a literal value to read from an environment variable at runtime.\x1B[0m")
	fmt.Println()

	name, err := promptUnique("Profile name", edited.Name, other)
	if err != nil {
		return err
	}
	edited.Name = name

	if edited.BaseURL != "" {
		if edited.BaseURL, err = menu.PromptLine("baseUrl", menu.PromptOptions{Default: edited.BaseURL, Prefill: true, Required: true}); err != nil {
			return err
		}
	}

	if edited.AuthToken != "" {
		if edited.AuthToken, err = menu.PromptLine("authToken (Bearer token)", menu.PromptOptions{Default: edited.AuthToken, Prefill: true, Required: true}); err != nil {
			return err
		}
	}
	if edited.APIKey != "" {
		if edited.APIKey, err = menu.PromptLine("apiKey", menu.PromptOptions{Default: edited.APIKey, Prefill: true, Required: true}); err != nil {
			return err
		}
	}

	switch {
	case isLM:
		ans, _ := menu.PromptLine("Re-fetch the model list? (y/N)", menu.PromptOptions{})
		if strings.EqualFold(strings.TrimSpace(ans), "y") {
			configureLMStudioModels(&edited)
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	case isOR:
		ans, _ := menu.PromptLine("Re-fetch the OpenRouter model list? (y/N)", menu.PromptOptions{})
		if strings.EqualFold(strings.TrimSpace(ans), "y") {
			configureOpenRouterModels(&edited)
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	case isNV:
		ans, _ := menu.PromptLine("Re-fetch the NVIDIA NIM model list? (y/N)", menu.PromptOptions{})
		if strings.EqualFold(strings.TrimSpace(ans), "y") {
			configureNVIDIAModels(&edited)
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	default:
		if edited.BaseURL != "" && (edited.AuthToken != "" || edited.APIKey != "") {
			ans, _ := menu.PromptLine("Re-fetch the model list? (y/N)", menu.PromptOptions{})
			if strings.EqualFold(strings.TrimSpace(ans), "y") {
				configureAnthropicModels(&edited)
			} else {
				edited.Models = promptModelsManual(edited.Models)
			}
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	}

	existing[index] = edited
	if err := config.Save(loaded.Path, config.Config{Profiles: existing}); err != nil {
		fmt.Printf("\n  \x1B[31msave failed: %s\x1B[0m\n", err)
	} else {
		fmt.Printf("\n  \x1B[32m✓\x1B[0m %q updated\n", edited.Name)
		fmt.Printf("  \x1B[90m%s\x1B[0m\n", loaded.Path)
	}
	fmt.Println()
	_, _ = menu.PromptLine("Press Enter to continue", menu.PromptOptions{})
	return nil
}

// Delete removes the profile at index after confirmation.
func Delete(loaded *config.Loaded, index int) error {
	existing := loaded.Config.Profiles
	if index < 0 || index >= len(existing) {
		return nil
	}
	target := existing[index]

	menu.ClearScreen()
	fmt.Println()
	fmt.Printf("  \x1B[1m\x1B[31m ccx \x1B[0m\x1B[90m— Delete profile\x1B[0m\n\n")
	fmt.Printf("  Name:        \x1B[1m%s\x1B[0m\n", target.Name)
	if target.BaseURL != "" {
		fmt.Printf("  baseUrl:     \x1B[90m%s\x1B[0m\n", target.BaseURL)
	}
	if target.Description != "" {
		fmt.Printf("  Description: \x1B[90m%s\x1B[0m\n", target.Description)
	}
	fmt.Println()
	fmt.Println("  \x1B[33m⚠ This action cannot be undone.\x1B[0m")
	fmt.Println()

	ans, _ := menu.PromptLine("Type y to delete, Enter to cancel", menu.PromptOptions{})
	if !strings.EqualFold(strings.TrimSpace(ans), "y") {
		fmt.Println("  \x1B[90mCanceled.\x1B[0m")
		fmt.Println()
		_, _ = menu.PromptLine("Press Enter to continue", menu.PromptOptions{})
		return nil
	}

	next := append(existing[:index:index], existing[index+1:]...)
	if err := config.Save(loaded.Path, config.Config{Profiles: next}); err != nil {
		fmt.Printf("\n  \x1B[31mdelete failed: %s\x1B[0m\n\n", err)
		_, _ = menu.PromptLine("Press Enter to continue", menu.PromptOptions{})
	}
	return nil
}

// ── Helpers ────────────────────────────────────────────────────────────

func nameSet(profiles []config.Profile) map[string]bool {
	out := map[string]bool{}
	for _, p := range profiles {
		out[strings.ToLower(p.Name)] = true
	}
	return out
}

func promptUnique(question, def string, taken map[string]bool) (string, error) {
	opts := menu.PromptOptions{Default: def, Prefill: def != "", Required: true}
	for {
		v, err := menu.PromptLine(question, opts)
		if err != nil {
			return "", err
		}
		if taken[strings.ToLower(v)] {
			fmt.Printf("  \x1B[31m%q already exists.\x1B[0m\n", v)
			continue
		}
		return v, nil
	}
}

type priorAuth struct {
	authToken string
	apiKey    string
}

func findPriorAuth(baseURL string, existing []config.Profile) priorAuth {
	host := extractHost(baseURL)
	if host == "" {
		return priorAuth{}
	}
	for _, p := range existing {
		if extractHost(p.BaseURL) == host {
			return priorAuth{authToken: p.AuthToken, apiKey: p.APIKey}
		}
	}
	return priorAuth{}
}

func extractHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func buildModels(opus, sonnet, haiku string) *config.Models {
	if opus == "" && sonnet == "" && haiku == "" {
		return nil
	}
	return &config.Models{Opus: opus, Sonnet: sonnet, Haiku: haiku}
}

func promptModelsManual(current *config.Models) *config.Models {
	cur := config.Models{}
	if current != nil {
		cur = *current
	}
	opus, _ := menu.PromptLine("Model opus", menu.PromptOptions{Default: cur.Opus, Prefill: true})
	sonnetDef := cur.Sonnet
	if sonnetDef == "" {
		sonnetDef = opus
	}
	sonnet, _ := menu.PromptLine("Model sonnet", menu.PromptOptions{Default: sonnetDef, Prefill: true})
	haikuDef := cur.Haiku
	if haikuDef == "" {
		if sonnet != "" {
			haikuDef = sonnet
		} else {
			haikuDef = opus
		}
	}
	haiku, _ := menu.PromptLine("Model haiku", menu.PromptOptions{Default: haikuDef, Prefill: true})
	return buildModels(opus, sonnet, haiku)
}

// ── Model catalog flows ────────────────────────────────────────────────

func configureLMStudioModels(tpl *config.Profile) {
	fmt.Println()
	fmt.Printf("  \x1B[36m[ccx]\x1B[0m Fetching model list... \x1B[90m(%s/v1/models)\x1B[0m\n", tpl.BaseURL)
	res := providers.FetchLMStudioModels(tpl.BaseURL, tpl.AuthToken)
	if res.Err != nil {
		fmt.Printf("  \x1B[33m⚠ fetch failed: %s\x1B[0m\n", res.Err)
		fmt.Println("  \x1B[90mEnter models manually.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	if len(res.Models) == 0 {
		fmt.Println("  \x1B[33m⚠ No models loaded. Load a model in LM Studio first.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d models found\n", len(res.Models))

	// 네이티브 API로 로드된 인스턴스의 실할당 컨텍스트를 조회해 suffix로 박제 —
	// launch 시 ctxwin이 이 표기를 해석해 Claude Code에 전달한다.
	ctxByModel := providers.FetchLMStudioContexts(tpl.BaseURL, tpl.AuthToken)

	items := make([]menu.CatalogItem, 0, len(res.Models))
	for _, m := range res.Models {
		desc := ""
		if w := ctxByModel[m]; w > 0 {
			desc = fmt.Sprintf("ctx %d (loaded)", w)
		}
		items = append(items, menu.CatalogItem{
			Label:       m,
			Description: desc,
			Payload:     m + providers.ContextSuffix(ctxByModel[m]),
		})
	}
	pickModelTiers(tpl, items)
}

func configureOpenRouterModels(tpl *config.Profile) {
	fmt.Println()
	fmt.Print("  \x1B[36m[ccx]\x1B[0m Fetching OpenRouter model list... \x1B[90m(https://openrouter.ai/api/v1/models)\x1B[0m\n")
	token := tpl.APIKey
	if token == "" {
		token = tpl.AuthToken
	}
	res := providers.FetchOpenRouterModels(token)
	if res.Err != nil || len(res.Models) == 0 {
		if res.Err != nil {
			fmt.Printf("  \x1B[33m⚠ fetch failed: %s\x1B[0m\n", res.Err)
		} else {
			fmt.Println("  \x1B[33m⚠ Model list is empty.\x1B[0m")
		}
		fmt.Println("  \x1B[90mEnter models manually.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d models found\n", len(res.Models))

	items := make([]menu.CatalogItem, 0, len(res.Models))
	for _, m := range res.Models {
		items = append(items, menu.CatalogItem{
			Label:       m.ID,
			Description: providers.FormatDescription(m),
			// 감지된 컨텍스트를 suffix로 박제 (모델/1순위 프로바이더 중 작은 값)
			Payload: m.ID + providers.ContextSuffix(providers.EffectiveContext(m)),
		})
	}
	pickModelTiers(tpl, items)
}

// configureNVIDIAModels는 NIM의 /v1/models를 조회해 권장 모델과의 교집합에서
// 티어를 고르게 한다. NIM 응답에는 컨텍스트 정보가 없고, 실서빙 한도가 모델 공식
// 스펙과도 다르며 프로바이더마다 갈리므로(GLM-5.2: z.ai 1M vs NIM 202K),
// providers의 실측 테이블 값을 suffix로 박제한다 — OpenRouter/LM Studio와 같은 방식.
func configureNVIDIAModels(tpl *config.Profile) {
	base := tpl.BaseURL
	if base == "" {
		base = providers.NVIDIABaseURL
	}
	fmt.Println()
	fmt.Printf("  \x1B[36m[ccx]\x1B[0m Fetching NVIDIA NIM model list... \x1B[90m(%s/models)\x1B[0m\n", strings.TrimRight(base, "/"))
	token := config.ResolveSecret(tpl.AuthToken)
	if token == "" {
		token = config.ResolveSecret(tpl.APIKey)
	}
	res := providers.FetchNVIDIAModels(base, token)
	if res.Err != nil || len(res.Models) == 0 {
		if res.Err != nil {
			fmt.Printf("  \x1B[33m⚠ fetch failed: %s\x1B[0m\n", res.Err)
		} else {
			fmt.Println("  \x1B[33m⚠ Model list is empty.\x1B[0m")
		}
		fmt.Println("  \x1B[90mEnter models manually.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}

	// 서버 목록 전체가 아니라 ccx 권장 모델과의 교집합만 보여준다 — 나머지는
	// Claude Code의 툴 콜링·긴 컨텍스트를 감당하지 못한다.
	models := providers.FilterRecommended(res.Models)
	if len(models) == 0 {
		fmt.Printf("  \x1B[33m⚠ None of the recommended models are available (%d listed by the server).\x1B[0m\n", len(res.Models))
		fmt.Println("  \x1B[90mEnter models manually.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d recommended models available \x1B[90m(%d others hidden)\x1B[0m\n", len(models), len(res.Models)-len(models))

	items := make([]menu.CatalogItem, 0, len(models))
	for _, m := range models {
		desc := ""
		if w := providers.NVIDIAContextWindow(m.ID); w > 0 {
			desc = fmt.Sprintf("ctx %dk", int(math.Round(float64(w)/1000)))
		}
		items = append(items, menu.CatalogItem{
			Label:       m.ID,
			Description: desc,
			Payload:     m.ID + providers.ContextSuffix(providers.NVIDIAContextWindow(m.ID)),
		})
	}
	pickModelTiers(tpl, items)
}

func configureAnthropicModels(tpl *config.Profile) {
	if tpl.BaseURL == "" || (tpl.AuthToken == "" && tpl.APIKey == "") {
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Println()
	fmt.Printf("  \x1B[36m[ccx]\x1B[0m Fetching model list... \x1B[90m(%s/v1/models)\x1B[0m\n", strings.TrimRight(tpl.BaseURL, "/"))
	res := providers.FetchAnthropicModels(tpl)
	if res.Err != nil || len(res.Models) == 0 {
		if res.Err != nil {
			fmt.Printf("  \x1B[33m⚠ fetch failed: %s\x1B[0m\n", res.Err)
		} else {
			fmt.Println("  \x1B[33m⚠ Model list is empty.\x1B[0m")
		}
		fmt.Println("  \x1B[90mThe provider may not expose /v1/models. Enter models manually.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d models found\n", len(res.Models))

	items := make([]menu.CatalogItem, 0, len(res.Models))
	for _, m := range res.Models {
		desc := ""
		if m.DisplayName != "" && m.DisplayName != m.ID {
			desc = m.DisplayName
		}
		items = append(items, menu.CatalogItem{
			Label:       m.ID,
			Description: desc,
			Payload:     m.ID,
		})
	}
	pickModelTiers(tpl, items)
}

// pickModelTiers walks opus → sonnet → haiku, letting the user pick a model
// for each tier from the same item list. Esc on a tier keeps the prior value.
func pickModelTiers(tpl *config.Profile, items []menu.CatalogItem) {
	skip := menu.CatalogItem{
		Label:       "(do not configure this tier)",
		Description: "Leave the env var unset — Claude Code default behavior",
		Payload:     "",
		Pinned:      true,
	}

	cur := config.Models{}
	if tpl.Models != nil {
		cur = *tpl.Models
	}
	next := config.Models{}
	for _, tier := range []struct {
		name string
		curr string
		set  func(string)
	}{
		{"opus", cur.Opus, func(s string) { next.Opus = s }},
		{"sonnet", cur.Sonnet, func(s string) { next.Sonnet = s }},
		{"haiku", cur.Haiku, func(s string) { next.Haiku = s }},
	} {
		title := "Select model for " + tier.name + " tier"
		if tier.curr != "" {
			title += " — current: " + tier.curr
		}
		menuItems := append([]menu.CatalogItem{}, items...)
		menuItems = append(menuItems, skip)
		picked, _ := menu.SelectFromCatalog(menuItems, title, 10)
		if picked == nil {
			if tier.curr != "" {
				tier.set(tier.curr)
			}
			continue
		}
		if s, ok := picked.(string); ok && s != "" {
			tier.set(s)
		}
	}
	if next.Opus == "" && next.Sonnet == "" && next.Haiku == "" {
		tpl.Models = nil
	} else {
		tpl.Models = &next
	}
}
