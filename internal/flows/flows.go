package flows

import (
	"fmt"
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
		fmt.Printf("  \x1B[33m⚠ 카탈로그 로드 실패: %s\x1B[0m\n", err)
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
		Label:       "기타 (직접 입력)",
		Description: "카탈로그에 없는 프로바이더를 수동으로 추가",
		Payload:     "manual",
		Pinned:      true,
	})

	picked, err := menu.SelectFromCatalog(items, "새 프로바이더 추가", 10)
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
		fmt.Printf("\n  \x1B[31m저장 실패: %s\x1B[0m\n", err)
	} else {
		fmt.Printf("\n  \x1B[32m✓\x1B[0m %q 저장됨\n", newProfile.Name)
		fmt.Printf("  \x1B[90m%s\x1B[0m\n", loaded.Path)
	}
	fmt.Println()
	_, _ = menu.PromptLine("Enter를 눌러 계속", menu.PromptOptions{})
	return nil
}

func customizeTemplate(tpl config.Profile, existing []config.Profile) (*config.Profile, error) {
	existingNames := nameSet(existing)
	isLM := providers.IsLMStudio(tpl.Name)
	isOR := providers.IsOpenRouter(&tpl)

	fmt.Println()
	fmt.Printf("  \x1B[36m[%s]\x1B[0m 설정\n", tpl.Name)
	fmt.Println("  \x1B[90m키 값 대신 env:VAR_NAME을 입력하면 실행 시 환경변수에서 읽습니다.\x1B[0m")
	fmt.Println()

	// 이름 — 중복이면 " (copy)" 제안
	suggested := tpl.Name
	if existingNames[strings.ToLower(suggested)] {
		suggested = tpl.Name + " (copy)"
	}
	name, err := promptUnique("프로파일 이름", suggested, existingNames)
	if err != nil {
		return nil, err
	}
	tpl.Name = name

	if isLM {
		if tpl.BaseURL, err = menu.PromptLine("baseUrl (엔드포인트)", menu.PromptOptions{Default: tpl.BaseURL, Prefill: true, Required: true}); err != nil {
			return nil, err
		}
	}

	prior := findPriorAuth(tpl.BaseURL, existing)
	if tpl.AuthToken != "" || prior.authToken != "" {
		def := prior.authToken
		if def == "" {
			def = tpl.AuthToken
		}
		if tpl.AuthToken, err = menu.PromptLine("authToken (Bearer 토큰)", menu.PromptOptions{Default: def, Prefill: true, Required: true}); err != nil {
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
	}

	return &tpl, nil
}

func addManual(existing []config.Profile) (*config.Profile, error) {
	fmt.Println()
	fmt.Println("  \x1B[90m필드를 하나씩 입력합니다. Ctrl+C로 취소.\x1B[0m")
	fmt.Println("  \x1B[90m키 값 대신 env:VAR_NAME을 입력하면 실행 시 환경변수에서 읽습니다.\x1B[0m")

	existingNames := nameSet(existing)
	name, err := promptUnique("프로파일 이름", "", existingNames)
	if err != nil {
		return nil, err
	}
	description, _ := menu.PromptLine("설명 (선택)", menu.PromptOptions{})
	baseURL, err := menu.PromptLine("baseUrl (예: https://api.example.com/anthropic)", menu.PromptOptions{Required: true})
	if err != nil {
		return nil, err
	}

	authType, err := menu.PromptChoice("인증 방식", []string{
		"authToken — Authorization: Bearer 헤더 (z.ai, Kimi, Ollama 등)",
		"apiKey — x-api-key 헤더 (DeepSeek, MiniMax, OpenRouter 등)",
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
		authValue, err = menu.PromptLine("인증 값", menu.PromptOptions{Default: priorVal, Prefill: true, Required: true})
	} else {
		authValue, err = menu.PromptLine("인증 값", menu.PromptOptions{Required: true})
	}
	if err != nil {
		return nil, err
	}

	opus, _ := menu.PromptLine("모델 opus (선택)", menu.PromptOptions{})
	sonnet, _ := menu.PromptLine("모델 sonnet (선택)", menu.PromptOptions{Default: opus})
	sonnetDef := sonnet
	if sonnetDef == "" {
		sonnetDef = opus
	}
	haiku, _ := menu.PromptLine("모델 haiku (선택)", menu.PromptOptions{Default: sonnetDef})

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

	menu.ClearScreen()
	fmt.Println()
	fmt.Printf("  \x1B[1m\x1B[36m ccx \x1B[0m\x1B[90m— 프로파일 편집: %s\x1B[0m\n", original.Name)
	fmt.Println("  \x1B[90mEnter로 기존 값 유지, Ctrl+U로 지우고 재입력\x1B[0m")
	fmt.Println("  \x1B[90m키 값 대신 env:VAR_NAME을 입력하면 실행 시 환경변수에서 읽습니다.\x1B[0m")
	fmt.Println()

	name, err := promptUnique("프로파일 이름", edited.Name, other)
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
		if edited.AuthToken, err = menu.PromptLine("authToken (Bearer 토큰)", menu.PromptOptions{Default: edited.AuthToken, Prefill: true, Required: true}); err != nil {
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
		ans, _ := menu.PromptLine("모델 목록을 다시 조회할까요? (y/N)", menu.PromptOptions{})
		if strings.EqualFold(strings.TrimSpace(ans), "y") {
			configureLMStudioModels(&edited)
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	case isOR:
		ans, _ := menu.PromptLine("OpenRouter 모델 목록을 다시 조회할까요? (y/N)", menu.PromptOptions{})
		if strings.EqualFold(strings.TrimSpace(ans), "y") {
			configureOpenRouterModels(&edited)
		} else {
			edited.Models = promptModelsManual(edited.Models)
		}
	default:
		edited.Models = promptModelsManual(edited.Models)
	}

	existing[index] = edited
	if err := config.Save(loaded.Path, config.Config{Profiles: existing}); err != nil {
		fmt.Printf("\n  \x1B[31m저장 실패: %s\x1B[0m\n", err)
	} else {
		fmt.Printf("\n  \x1B[32m✓\x1B[0m %q 수정됨\n", edited.Name)
		fmt.Printf("  \x1B[90m%s\x1B[0m\n", loaded.Path)
	}
	fmt.Println()
	_, _ = menu.PromptLine("Enter를 눌러 계속", menu.PromptOptions{})
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
	fmt.Printf("  \x1B[1m\x1B[31m ccx \x1B[0m\x1B[90m— 프로파일 삭제\x1B[0m\n\n")
	fmt.Printf("  이름:    \x1B[1m%s\x1B[0m\n", target.Name)
	if target.BaseURL != "" {
		fmt.Printf("  baseUrl: \x1B[90m%s\x1B[0m\n", target.BaseURL)
	}
	if target.Description != "" {
		fmt.Printf("  설명:    \x1B[90m%s\x1B[0m\n", target.Description)
	}
	fmt.Println()
	fmt.Println("  \x1B[33m⚠ 이 작업은 되돌릴 수 없습니다.\x1B[0m")
	fmt.Println()

	ans, _ := menu.PromptLine("삭제하려면 y 입력, 취소는 Enter", menu.PromptOptions{})
	if !strings.EqualFold(strings.TrimSpace(ans), "y") {
		fmt.Println("  \x1B[90m취소되었습니다.\x1B[0m")
		fmt.Println()
		_, _ = menu.PromptLine("Enter를 눌러 계속", menu.PromptOptions{})
		return nil
	}

	next := append(existing[:index:index], existing[index+1:]...)
	if err := config.Save(loaded.Path, config.Config{Profiles: next}); err != nil {
		fmt.Printf("\n  \x1B[31m삭제 실패: %s\x1B[0m\n\n", err)
		_, _ = menu.PromptLine("Enter를 눌러 계속", menu.PromptOptions{})
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
			fmt.Printf("  \x1B[31m%q은(는) 이미 존재합니다.\x1B[0m\n", v)
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
	opus, _ := menu.PromptLine("모델 opus", menu.PromptOptions{Default: cur.Opus, Prefill: true})
	sonnetDef := cur.Sonnet
	if sonnetDef == "" {
		sonnetDef = opus
	}
	sonnet, _ := menu.PromptLine("모델 sonnet", menu.PromptOptions{Default: sonnetDef, Prefill: true})
	haikuDef := cur.Haiku
	if haikuDef == "" {
		if sonnet != "" {
			haikuDef = sonnet
		} else {
			haikuDef = opus
		}
	}
	haiku, _ := menu.PromptLine("모델 haiku", menu.PromptOptions{Default: haikuDef, Prefill: true})
	return buildModels(opus, sonnet, haiku)
}

// ── Model catalog flows ────────────────────────────────────────────────

func configureLMStudioModels(tpl *config.Profile) {
	fmt.Println()
	fmt.Printf("  \x1B[36m[ccx]\x1B[0m 모델 목록 조회 중... \x1B[90m(%s/v1/models)\x1B[0m\n", tpl.BaseURL)
	res := providers.FetchLMStudioModels(tpl.BaseURL, tpl.AuthToken)
	if res.Err != nil {
		fmt.Printf("  \x1B[33m⚠ 조회 실패: %s\x1B[0m\n", res.Err)
		fmt.Println("  \x1B[90m모델을 수동으로 입력하세요.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	if len(res.Models) == 0 {
		fmt.Println("  \x1B[33m⚠ 로드된 모델이 없습니다. LM Studio에서 모델을 먼저 로드하세요.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d개 모델 발견\n", len(res.Models))

	baseItems := make([]menu.CatalogItem, 0, len(res.Models)+1)
	for _, m := range res.Models {
		baseItems = append(baseItems, menu.CatalogItem{Label: m, Payload: m})
	}
	skip := menu.CatalogItem{
		Label:       "(이 티어는 설정하지 않음)",
		Description: "환경변수 미지정 — Claude Code 기본 동작",
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
		title := tier.name + " 티어 모델 선택"
		if tier.curr != "" {
			title += " — 현재: " + tier.curr
		}
		items := append([]menu.CatalogItem{}, baseItems...)
		items = append(items, skip)
		picked, _ := menu.SelectFromCatalog(items, title, 10)
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

func configureOpenRouterModels(tpl *config.Profile) {
	fmt.Println()
	fmt.Print("  \x1B[36m[ccx]\x1B[0m OpenRouter 모델 목록 조회 중... \x1B[90m(https://openrouter.ai/api/v1/models)\x1B[0m\n")
	token := tpl.APIKey
	if token == "" {
		token = tpl.AuthToken
	}
	res := providers.FetchOpenRouterModels(token)
	if res.Err != nil || len(res.Models) == 0 {
		if res.Err != nil {
			fmt.Printf("  \x1B[33m⚠ 조회 실패: %s\x1B[0m\n", res.Err)
		} else {
			fmt.Println("  \x1B[33m⚠ 모델 목록이 비어 있습니다.\x1B[0m")
		}
		fmt.Println("  \x1B[90m모델을 수동으로 입력하세요.\x1B[0m")
		tpl.Models = promptModelsManual(tpl.Models)
		return
	}
	fmt.Printf("  \x1B[32m✓\x1B[0m %d개 모델 발견\n", len(res.Models))

	baseItems := make([]menu.CatalogItem, 0, len(res.Models)+1)
	for _, m := range res.Models {
		baseItems = append(baseItems, menu.CatalogItem{
			Label:       m.ID,
			Description: providers.FormatDescription(m),
			Payload:     m.ID,
		})
	}
	skip := menu.CatalogItem{
		Label:       "(이 티어는 설정하지 않음)",
		Description: "환경변수 미지정 — Claude Code 기본 동작",
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
		title := tier.name + " 티어 모델 선택"
		if tier.curr != "" {
			title += " — 현재: " + tier.curr
		}
		items := append([]menu.CatalogItem{}, baseItems...)
		items = append(items, skip)
		picked, _ := menu.SelectFromCatalog(items, title, 10)
		if picked == nil {
			// Esc: 기존 값 유지
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
