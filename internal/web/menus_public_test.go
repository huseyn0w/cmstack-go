package web

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/content/menus"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

type fakeMenuPublic struct {
	byLoc  map[string][]menus.ResolvedItem
	locale i18n.Locale
}

func (f *fakeMenuPublic) ResolveForLocation(_ context.Context, location string, locale i18n.Locale) ([]menus.ResolvedItem, error) {
	f.locale = locale
	return f.byLoc[location], nil
}

func TestToMenuLinks_MapsRecursively(t *testing.T) {
	in := []menus.ResolvedItem{
		{Label: "Blog", URL: "/blog", Children: []menus.ResolvedItem{
			{Label: "Go", URL: "/categories/go"},
		}},
		{Label: "About", URL: "/p/about"},
	}
	out := toMenuLinks(in)
	if len(out) != 2 || out[0].Label != "Blog" || out[0].URL != "/blog" {
		t.Fatalf("top-level mapping wrong: %+v", out)
	}
	if len(out[0].Children) != 1 || out[0].Children[0].URL != "/categories/go" {
		t.Fatalf("child mapping wrong: %+v", out[0].Children)
	}
	if toMenuLinks(nil) != nil {
		t.Fatal("empty input should map to nil")
	}
}

func TestMenuPublicSource_ReadsLocaleAndMaps(t *testing.T) {
	fake := &fakeMenuPublic{byLoc: map[string][]menus.ResolvedItem{
		"header": {{Label: "Home", URL: "/"}},
	}}
	src := menuPublicSource{svc: fake}
	links := src.MenuForLocation(context.Background(), "header")
	if len(links) != 1 || links[0].Label != "Home" {
		t.Fatalf("unexpected links: %+v", links)
	}
	if fake.locale != i18n.Default() {
		t.Fatalf("locale = %q, want default", fake.locale)
	}
}

func TestPublicLayout_RendersManagedMenus(t *testing.T) {
	fake := &fakeMenuPublic{byLoc: map[string][]menus.ResolvedItem{
		"header": {{Label: "Services", URL: "/services"}},
		"footer": {{Label: "Privacy", URL: "/p/privacy"}},
	}}
	webtempl.SetMenuSource(menuPublicSource{svc: fake})
	defer webtempl.SetMenuSource(nil)

	body, err := render.ToString(context.Background(), webtempl.Base(webtempl.LayoutData{Title: "T"}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, `data-testid="header-menu"`) || !strings.Contains(body, `href="/services"`) {
		t.Error("header menu not rendered")
	}
	if !strings.Contains(body, `data-testid="footer-menu"`) || !strings.Contains(body, `href="/p/privacy"`) {
		t.Error("footer menu not rendered")
	}
}

func TestPublicLayout_NoMenuSourceOmitsMenus(t *testing.T) {
	webtempl.SetMenuSource(nil)
	body, err := render.ToString(context.Background(), webtempl.Base(webtempl.LayoutData{Title: "T"}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, `data-testid="header-menu"`) {
		t.Error("header menu should be absent with no source")
	}
}
