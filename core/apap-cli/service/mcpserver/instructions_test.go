// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
)

func TestStaticTextResourceHandler(t *testing.T) {
	const uri = "apx://instructions/run-defaults"
	const text = "# Run Defaults\n\nUse defaults."

	result, err := staticTextResourceHandler(uri, text)(context.Background(), nil)
	if err != nil {
		t.Fatalf("staticTextResourceHandler returned error: %v", err)
	}
	if result == nil {
		t.Fatal("staticTextResourceHandler returned nil result")
		return
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected one resource content, got %d", len(result.Contents))
	}

	content := result.Contents[0]
	if content.URI != uri {
		t.Fatalf("expected URI %q, got %q", uri, content.URI)
	}
	if content.MIMEType != instructionsResourceMIMEType {
		t.Fatalf("expected MIME type %q, got %q", instructionsResourceMIMEType, content.MIMEType)
	}
	if content.Text != text {
		t.Fatalf("expected text %q, got %q", text, content.Text)
	}
}

func TestSlugifyHeading(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "hyphenates title words",
			title: "Analysis Follow-ups",
			want:  "analysis-follow-ups",
		},
		{
			name:  "collapses punctuation and whitespace",
			title: "  Recipe: Guide!  ",
			want:  "recipe-guide",
		},
		{
			name:  "keeps digits",
			title: "CPU & Memory 101",
			want:  "cpu-memory-101",
		},
		{
			name:  "trims separators",
			title: "---Run Defaults---",
			want:  "run-defaults",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugifyHeading(tt.title); got != tt.want {
				t.Fatalf("slugifyHeading(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestTopLevelHeading(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantTitle string
		wantOK    bool
	}{
		{
			name:      "level one heading",
			line:      "# Run Defaults",
			wantTitle: "Run Defaults",
			wantOK:    true,
		},
		{
			name:      "trims title whitespace",
			line:      "#   Recipe Guide  ",
			wantTitle: "Recipe Guide",
			wantOK:    true,
		},
		{
			name:   "ignores nested heading",
			line:   "## code_hotspots",
			wantOK: false,
		},
		{
			name:   "ignores missing heading space",
			line:   "#Run Defaults",
			wantOK: false,
		},
		{
			name:   "ignores indented heading",
			line:   " # Run Defaults",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotOK := topLevelHeading(tt.line)
			if gotOK != tt.wantOK {
				t.Fatalf("topLevelHeading(%q) ok = %t, want %t", tt.line, gotOK, tt.wantOK)
			}
			if gotTitle != tt.wantTitle {
				t.Fatalf("topLevelHeading(%q) title = %q, want %q", tt.line, gotTitle, tt.wantTitle)
			}
		})
	}
}

func TestParseInstructionSections(t *testing.T) {
	const md = `Intro before the first heading is ignored.

# Run Defaults

- Default to hotspots.

---

# Recipe: Guide!

## code_hotspots

Use this as the default recipe.

---`

	sections := parseInstructionSections(md)
	if len(sections) != 2 {
		t.Fatalf("expected two sections, got %d", len(sections))
	}

	assertInstructionSection(t, sections[0], instructionSection{
		title: "Run Defaults",
		slug:  "run-defaults",
		body:  "# Run Defaults\n\n- Default to hotspots.",
	})
	assertInstructionSection(t, sections[1], instructionSection{
		title: "Recipe: Guide!",
		slug:  "recipe-guide",
		body:  "# Recipe: Guide!\n\n## code_hotspots\n\nUse this as the default recipe.",
	})
}

func TestParseInstructionSectionsWithoutHeadings(t *testing.T) {
	sections := parseInstructionSections("Intro only.\n\n## Nested only")
	if len(sections) != 0 {
		t.Fatalf("expected no sections, got %d", len(sections))
	}
}

func TestInstructionSectionSlugsAreUnique(t *testing.T) {
	sections := parseInstructionSections(instructions)
	if len(sections) == 0 {
		t.Fatal("expected embedded instructions to contain at least one section")
	}

	seen := make(map[string]string, len(sections))
	for _, section := range sections {
		if section.title == "" {
			t.Fatal("embedded instructions produced a section with an empty title")
		}
		if section.body == "" {
			t.Fatalf("instruction section %q produced an empty body", section.title)
		}
		if want := "# " + section.title; !strings.HasPrefix(section.body, want) {
			t.Fatalf("instruction section %q body should start with its heading %q", section.title, want)
		}
		if section.slug == "" {
			t.Fatalf("instruction section %q generated an empty slug", section.title)
		}
		if previousTitle, ok := seen[section.slug]; ok {
			t.Fatalf("instruction sections %q and %q both generate slug %q", previousTitle, section.title, section.slug)
		}
		seen[section.slug] = section.title
	}
}

func assertInstructionSection(t *testing.T, got, want instructionSection) {
	t.Helper()

	if got.title != want.title {
		t.Fatalf("section title = %q, want %q", got.title, want.title)
	}
	if got.slug != want.slug {
		t.Fatalf("section slug = %q, want %q", got.slug, want.slug)
	}
	if got.body != want.body {
		t.Fatalf("section body = %q, want %q", got.body, want.body)
	}
}
