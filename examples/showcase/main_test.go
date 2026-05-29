package main

import (
	"strings"
	"testing"
	"time"
)

func TestRenderAllLanguages(t *testing.T) {
	rt, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	for i, lang := range languages {
		t.Run(lang.Tag, func(t *testing.T) {
			model := appModel{
				runtime:       rt,
				selected:      i,
				count:         3,
				billingDomain: true,
				now:           time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC),
			}
			screen := render(&model)
			for _, want := range []string{"go-i18n ::", "CLDR", "A-1042", `"field":"email"`} {
				if !strings.Contains(screen, want) {
					t.Fatalf("render(%s) missing %q:\n%s", lang.Tag, want, screen)
				}
			}
			for _, notWant := range []string{"length-kilometer", "duration-hour"} {
				if strings.Contains(screen, notWant) {
					t.Fatalf("render(%s) exposed unsupported unit token %q:\n%s", lang.Tag, notWant, screen)
				}
			}
		})
	}
}

func TestCommandsSwitchLanguageAndState(t *testing.T) {
	model := appModel{selected: 0, count: 1}
	if handleCommand(&model, "6") {
		t.Fatal("language switch should not quit")
	}
	if model.selected != 5 {
		t.Fatalf("selected = %d, want Arabic", model.selected)
	}
	handleCommand(&model, "n")
	handleCommand(&model, "+")
	if model.count != 3 {
		t.Fatalf("count = %d, want 3", model.count)
	}
	handleCommand(&model, "p")
	if model.count != 2 {
		t.Fatalf("count = %d, want 2", model.count)
	}
	handleCommand(&model, "d")
	if !model.billingDomain {
		t.Fatal("billing domain was not toggled")
	}
	if handleCommand(&model, "q") != true {
		t.Fatal("q should quit")
	}
}

func TestCatalogPluralForms(t *testing.T) {
	rt, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	arProfile, _, err := profileFor(languages[5])
	if err != nil {
		t.Fatal(err)
	}
	ar := rt.Translator(arProfile)
	if got := ar.Tn(2, "{n} task", "{n} tasks"); !strings.Contains(got, "مهمتان") {
		t.Fatalf("Arabic dual plural = %q", got)
	}
	if got := ar.Tn(3, "{n} task", "{n} tasks"); !strings.Contains(got, "مهام") {
		t.Fatalf("Arabic few plural = %q", got)
	}

	zhProfile, _, err := profileFor(languages[4])
	if err != nil {
		t.Fatal(err)
	}
	zh := rt.Translator(zhProfile)
	one := zh.Tn(1, "{n} task", "{n} tasks")
	many := zh.Tn(9, "{n} task", "{n} tasks")
	if !strings.Contains(one, "任务") || !strings.Contains(many, "任务") {
		t.Fatalf("Chinese plural forms = %q / %q", one, many)
	}
}

func TestArabicRenderUsesProfileNumberingAndShowsDiagnostics(t *testing.T) {
	rt, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	model := appModel{
		runtime:  rt,
		selected: 5,
		count:    3,
		now:      time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC),
	}
	screen := render(&model)
	for _, want := range []string{"1,234.50", "16", "ج.م."} {
		if !strings.Contains(screen, want) {
			t.Fatalf("Arabic render missing %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "currency_symbol_unavailable") {
		t.Fatalf("Arabic render reported obsolete currency diagnostic:\n%s", screen)
	}
	for _, notWant := range []string{"١٬٢٣٤٫٥٠", "١٦"} {
		if strings.Contains(screen, notWant) {
			t.Fatalf("Arabic render ignored profile numbering %q:\n%s", notWant, screen)
		}
	}
}

func TestRunProcessesInput(t *testing.T) {
	rt, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	model := appModel{
		runtime: rt,
		count:   1,
		now:     time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC),
	}
	var out strings.Builder
	if err := run(strings.NewReader("de\nd\nq\n"), &out, &model); err != nil {
		t.Fatal(err)
	}
	if model.selected != 1 || !model.billingDomain {
		t.Fatalf("model after run = selected %d billing %v", model.selected, model.billingDomain)
	}
	if !strings.Contains(out.String(), "Go-i18n-Schaukasten") {
		t.Fatalf("run output did not include German screen:\n%s", out.String())
	}
}
