package web

import (
	"context"
	"strings"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// SiteOverrideReader reads admin-editable site/SEO override values from the
// settings store. An empty value ("") means "no override — use the config
// default". *settings.Service satisfies it.
type SiteOverrideReader interface {
	Get(ctx context.Context, key string) (string, bool, error)
}

// Editable settings keys (snake_case). Each is an admin-editable override that,
// when non-empty, wins over the corresponding boot value from config; an empty
// stored value falls back to the config default. General keys are owned by the
// General dashboard; the SEO/GEO + Org + verification + analytics keys by the
// SEO & GEO dashboard. The contact recipient reuses the existing contact key.
const (
	// keySiteName overrides the site name (General).
	keySiteName = "site_name"
	// keySiteDescription overrides the site meta description (General).
	keySiteDescription = "site_description"
	// keySiteDefaultOGImage overrides the default Open Graph image (General).
	keySiteDefaultOGImage = "site_default_og_image"
	// keySiteTwitterHandle overrides the Twitter/X handle (General).
	keySiteTwitterHandle = "site_twitter_handle"

	// keySEOGlobalNoindex overrides the site-wide noindex gate (SEO & GEO).
	keySEOGlobalNoindex = "seo_global_noindex"
	// keySEOAllowAICrawlers overrides the AI-crawler allow flag (SEO & GEO).
	keySEOAllowAICrawlers = "seo_allow_ai_crawlers"
	// keySEOGoogleVerification overrides the Google verification token (SEO & GEO).
	keySEOGoogleVerification = "seo_google_verification"
	// keySEOBingVerification overrides the Bing verification token (SEO & GEO).
	keySEOBingVerification = "seo_bing_verification"
	// keySEOYandexVerification overrides the Yandex verification token (SEO & GEO).
	keySEOYandexVerification = "seo_yandex_verification"
	// keySEOPinterestVerification overrides the Pinterest verification token (SEO & GEO).
	keySEOPinterestVerification = "seo_pinterest_verification"

	// keyOrgName overrides the Organization name (SEO & GEO).
	keyOrgName = "org_name"
	// keyOrgLegalName overrides the Organization legal name (SEO & GEO).
	keyOrgLegalName = "org_legal_name"
	// keyOrgLogo overrides the Organization logo URL/path (SEO & GEO).
	keyOrgLogo = "org_logo"
	// keyOrgEmail overrides the Organization contact email (SEO & GEO).
	keyOrgEmail = "org_email"
	// keyOrgPhone overrides the Organization contact phone (SEO & GEO).
	keyOrgPhone = "org_phone"
	// keyOrgStreet overrides the Organization street address (SEO & GEO).
	keyOrgStreet = "org_street"
	// keyOrgLocality overrides the Organization locality/city (SEO & GEO).
	keyOrgLocality = "org_locality"
	// keyOrgRegion overrides the Organization region/state (SEO & GEO).
	keyOrgRegion = "org_region"
	// keyOrgPostalCode overrides the Organization postal code (SEO & GEO).
	keyOrgPostalCode = "org_postal_code"
	// keyOrgCountry overrides the Organization country (SEO & GEO).
	keyOrgCountry = "org_country"
	// keyOrgSameAs overrides the Organization sameAs URLs (newline-separated) (SEO & GEO).
	keyOrgSameAs = "org_same_as"
	// keyOrgGeoStatement overrides the Organization GEO statement (SEO & GEO).
	keyOrgGeoStatement = "org_geo_statement"
)

// SiteProfileKeys is the stable set of settings keys backing the admin-editable
// site/SEO/Org/GEO profile plus the two analytics container ids. It is the SINGLE
// exported source of truth the REST API's SEO-profile endpoints read/write
// through, so the API never hardcodes duplicate key literals that could drift
// from the M15 dashboards. Each field mirrors the corresponding unexported key
// constant (analytics ids come from analytics.go).
type SiteProfileKeys struct {
	SiteName                 string
	SiteDescription          string
	SiteDefaultOGImage       string
	SiteTwitterHandle        string
	SEOGlobalNoindex         string
	SEOAllowAICrawlers       string
	SEOGoogleVerification    string
	SEOBingVerification      string
	SEOYandexVerification    string
	SEOPinterestVerification string
	AnalyticsGA4ID           string
	AnalyticsGTMID           string
	OrgName                  string
	OrgLegalName             string
	OrgLogo                  string
	OrgEmail                 string
	OrgPhone                 string
	OrgStreet                string
	OrgLocality              string
	OrgRegion                string
	OrgPostalCode            string
	OrgCountry               string
	OrgSameAs                string
	OrgGeoStatement          string
}

// ProfileKeys returns the site-profile settings key set. Callers (the REST API)
// read/write settings through these values rather than re-declaring the literals.
func ProfileKeys() SiteProfileKeys {
	return SiteProfileKeys{
		SiteName:                 keySiteName,
		SiteDescription:          keySiteDescription,
		SiteDefaultOGImage:       keySiteDefaultOGImage,
		SiteTwitterHandle:        keySiteTwitterHandle,
		SEOGlobalNoindex:         keySEOGlobalNoindex,
		SEOAllowAICrawlers:       keySEOAllowAICrawlers,
		SEOGoogleVerification:    keySEOGoogleVerification,
		SEOBingVerification:      keySEOBingVerification,
		SEOYandexVerification:    keySEOYandexVerification,
		SEOPinterestVerification: keySEOPinterestVerification,
		AnalyticsGA4ID:           keyAnalyticsGA4ID,
		AnalyticsGTMID:           keyAnalyticsGTMID,
		OrgName:                  keyOrgName,
		OrgLegalName:             keyOrgLegalName,
		OrgLogo:                  keyOrgLogo,
		OrgEmail:                 keyOrgEmail,
		OrgPhone:                 keyOrgPhone,
		OrgStreet:                keyOrgStreet,
		OrgLocality:              keyOrgLocality,
		OrgRegion:                keyOrgRegion,
		OrgPostalCode:            keyOrgPostalCode,
		OrgCountry:               keyOrgCountry,
		OrgSameAs:                keyOrgSameAs,
		OrgGeoStatement:          keyOrgGeoStatement,
	}
}

// WithOverrides returns a copy of the SiteConfig wired to the given override
// reader. It mirrors the WithSite value-copy idiom: the reader is an interface
// (a reference under the hood), so every downstream value-copy of the returned
// SiteConfig shares the same live reader and reflects settings writes on the
// next read.
func (s SiteConfig) WithOverrides(r SiteOverrideReader) SiteConfig {
	s.overrides = r
	return s
}

// override returns the trimmed non-empty override value for key, or ("", false)
// when there is no override. It is nil-safe (a nil reader yields no override)
// and read-error safe (a store error is treated as "no override" so the config
// default applies rather than surfacing an error on a public render).
func (s SiteConfig) override(ctx context.Context, key string) (string, bool) {
	if s.overrides == nil {
		return "", false
	}
	v, ok, err := s.overrides.Get(ctx, key)
	if err != nil || !ok {
		return "", false
	}
	if v = strings.TrimSpace(v); v == "" {
		return "", false
	}
	return v, true
}

// stringOverride returns the override for key when set, else the config default.
func (s SiteConfig) stringOverride(ctx context.Context, key, def string) string {
	if v, ok := s.override(ctx, key); ok {
		return v
	}
	return def
}

// boolOverride parses the override for key ("1"/"true"/"on"/"yes" → true,
// "0"/"false"/"off"/"no" → false), falling back to def when unset or
// unparseable.
func (s SiteConfig) boolOverride(ctx context.Context, key string, def bool) bool {
	v, ok := s.override(ctx, key)
	if !ok {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	default:
		return def
	}
}

// resolveSiteName returns the effective site name (override || config).
func (s SiteConfig) resolveSiteName(ctx context.Context) string {
	return s.stringOverride(ctx, keySiteName, s.SiteName)
}

// resolveSiteDescription returns the effective description (override || config).
func (s SiteConfig) resolveSiteDescription(ctx context.Context) string {
	return s.stringOverride(ctx, keySiteDescription, s.SiteDescription)
}

// resolveDefaultOGImage returns the effective default OG image (override || config).
func (s SiteConfig) resolveDefaultOGImage(ctx context.Context) string {
	return s.stringOverride(ctx, keySiteDefaultOGImage, s.DefaultOGImage)
}

// resolveTwitterHandle returns the effective Twitter handle (override || config).
func (s SiteConfig) resolveTwitterHandle(ctx context.Context) string {
	return s.stringOverride(ctx, keySiteTwitterHandle, s.TwitterHandle)
}

// resolveGlobalNoindex returns the effective global-noindex gate (override || config).
func (s SiteConfig) resolveGlobalNoindex(ctx context.Context) bool {
	return s.boolOverride(ctx, keySEOGlobalNoindex, s.GlobalNoindex)
}

// resolveAllowAICrawlers returns the effective AI-crawler allow flag (override || config).
func (s SiteConfig) resolveAllowAICrawlers(ctx context.Context) bool {
	return s.boolOverride(ctx, keySEOAllowAICrawlers, s.AllowAICrawlers)
}

// resolveVerifications overlays the settings verification tokens on the boot
// values, emitting a MetaTag per non-empty effective token. The boot tags are
// the fallback source: for each engine, an override wins when set, else the
// boot value (extracted from s.Verifications) is used.
func (s SiteConfig) resolveVerifications(ctx context.Context) []webtempl.MetaTag {
	boot := func(name string) string {
		for _, m := range s.Verifications {
			if m.Name == name {
				return m.Content
			}
		}
		return ""
	}
	var out []webtempl.MetaTag
	add := func(name, key string) {
		content := s.stringOverride(ctx, key, boot(name))
		if content != "" {
			out = append(out, webtempl.MetaTag{Name: name, Content: content})
		}
	}
	add("google-site-verification", keySEOGoogleVerification)
	add("msvalidate.01", keySEOBingVerification)
	add("yandex-verification", keySEOYandexVerification)
	add("p:domain_verify", keySEOPinterestVerification)
	return out
}

// resolveOrg overlays the settings Org-identity values on the boot Org. Each
// field falls back to the boot value when its override is unset. Name falls back
// to the effective site name (never empty). SameAs is newline-separated in
// settings; the logo path is absolutized against BaseURL like the boot value.
func (s SiteConfig) resolveOrg(ctx context.Context) webtempl.OrgIdentity {
	org := s.Org

	name := s.stringOverride(ctx, keyOrgName, org.Name)
	if name == "" {
		name = s.resolveSiteName(ctx)
	}
	org.Name = name
	org.LegalName = s.stringOverride(ctx, keyOrgLegalName, org.LegalName)
	if logo, ok := s.override(ctx, keyOrgLogo); ok {
		org.LogoURL = s.absolutizeIfRooted(logo)
	}
	org.Email = s.stringOverride(ctx, keyOrgEmail, org.Email)
	org.Phone = s.stringOverride(ctx, keyOrgPhone, org.Phone)
	org.Street = s.stringOverride(ctx, keyOrgStreet, org.Street)
	org.Locality = s.stringOverride(ctx, keyOrgLocality, org.Locality)
	org.Region = s.stringOverride(ctx, keyOrgRegion, org.Region)
	org.PostalCode = s.stringOverride(ctx, keyOrgPostalCode, org.PostalCode)
	org.Country = s.stringOverride(ctx, keyOrgCountry, org.Country)
	org.GeoStatement = s.stringOverride(ctx, keyOrgGeoStatement, org.GeoStatement)
	if same, ok := s.override(ctx, keyOrgSameAs); ok {
		org.SameAs = splitSameAs(same)
	}
	return org
}

// splitSameAs splits a newline-separated sameAs block into a trimmed,
// empty-line-free slice of URLs.
func splitSameAs(v string) []string {
	lines := strings.Split(v, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
