package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/huseyn0w/agentic-cms-go/internal/web"
)

// profileKeys is the single source of truth for the site-profile settings keys,
// read from the web layer so the API never re-declares the M15 key literals.
var profileKeys = web.ProfileKeys()

// siteSection is the site/general block of the SEO profile.
type siteSection struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultOGImage string `json:"defaultOgImage"`
	TwitterHandle  string `json:"twitterHandle"`
}

// indexingSection is the indexing/crawler block of the SEO profile.
type indexingSection struct {
	GlobalNoindex   bool `json:"globalNoindex"`
	AllowAiCrawlers bool `json:"allowAiCrawlers"`
}

// verificationSection is the search-engine verification-token block.
type verificationSection struct {
	Google    string `json:"google"`
	Bing      string `json:"bing"`
	Yandex    string `json:"yandex"`
	Pinterest string `json:"pinterest"`
}

// analyticsSection is the analytics container-id block.
type analyticsSection struct {
	GA4ID string `json:"ga4Id"`
	GTMID string `json:"gtmId"`
}

// organizationSection is the Organization identity + GEO block.
type organizationSection struct {
	Name         string   `json:"name"`
	LegalName    string   `json:"legalName"`
	Logo         string   `json:"logo"`
	Email        string   `json:"email"`
	Phone        string   `json:"phone"`
	Street       string   `json:"street"`
	Locality     string   `json:"locality"`
	Region       string   `json:"region"`
	PostalCode   string   `json:"postalCode"`
	Country      string   `json:"country"`
	SameAs       []string `json:"sameAs"`
	GeoStatement string   `json:"geoStatement"`
}

// siteProfileDTO is the stable, structured JSON shape of the site/SEO/Org/GEO
// override profile. Every field reflects the currently-stored override value
// (empty when unset).
type siteProfileDTO struct {
	Site         siteSection         `json:"site"`
	Indexing     indexingSection     `json:"indexing"`
	Verification verificationSection `json:"verification"`
	Analytics    analyticsSection    `json:"analytics"`
	Organization organizationSection `json:"organization"`
}

// getSEOProfile serves GET /api/v1/seo/profile: the current override values,
// grouped into the structured profile DTO.
func (h *handler) getSEOProfile(w http.ResponseWriter, r *http.Request) {
	dto, err := h.readProfile(r.Context())
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to read seo profile")
		return
	}
	OK(w, http.StatusOK, dto)
}

// readProfile assembles the profile DTO from the settings store.
func (h *handler) readProfile(ctx context.Context) (siteProfileDTO, error) {
	get := func(key string) (string, error) {
		v, ok, err := h.settings.Get(ctx, key)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", nil
		}
		return v, nil
	}

	var (
		dto siteProfileDTO
		err error
	)
	str := func(key string) string {
		if err != nil {
			return ""
		}
		var v string
		v, err = get(key)
		return v
	}
	boolVal := func(key string) bool {
		return parseProfileBool(str(key))
	}

	k := profileKeys
	dto.Site = siteSection{
		Name:           str(k.SiteName),
		Description:    str(k.SiteDescription),
		DefaultOGImage: str(k.SiteDefaultOGImage),
		TwitterHandle:  str(k.SiteTwitterHandle),
	}
	dto.Indexing = indexingSection{
		GlobalNoindex:   boolVal(k.SEOGlobalNoindex),
		AllowAiCrawlers: boolVal(k.SEOAllowAICrawlers),
	}
	dto.Verification = verificationSection{
		Google:    str(k.SEOGoogleVerification),
		Bing:      str(k.SEOBingVerification),
		Yandex:    str(k.SEOYandexVerification),
		Pinterest: str(k.SEOPinterestVerification),
	}
	dto.Analytics = analyticsSection{
		GA4ID: str(k.AnalyticsGA4ID),
		GTMID: str(k.AnalyticsGTMID),
	}
	dto.Organization = organizationSection{
		Name:         str(k.OrgName),
		LegalName:    str(k.OrgLegalName),
		Logo:         str(k.OrgLogo),
		Email:        str(k.OrgEmail),
		Phone:        str(k.OrgPhone),
		Street:       str(k.OrgStreet),
		Locality:     str(k.OrgLocality),
		Region:       str(k.OrgRegion),
		PostalCode:   str(k.OrgPostalCode),
		Country:      str(k.OrgCountry),
		SameAs:       splitSameAs(str(k.OrgSameAs)),
		GeoStatement: str(k.OrgGeoStatement),
	}
	return dto, err
}

// parseProfileBool parses a stored override flag ("1"/"true"/"on"/"yes" -> true).
func parseProfileBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// splitSameAs splits a stored newline-separated sameAs block into a trimmed,
// empty-line-free slice (mirrors the web overlay's split).
func splitSameAs(v string) []string {
	lines := strings.Split(v, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// updateSiteSection is the partial site block. Pointer fields distinguish
// "provided" (write, even when empty = clear) from "omitted" (leave unchanged).
type updateSiteSection struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	DefaultOGImage *string `json:"defaultOgImage"`
	TwitterHandle  *string `json:"twitterHandle"`
}

// updateIndexingSection is the partial indexing block.
type updateIndexingSection struct {
	GlobalNoindex   *bool `json:"globalNoindex"`
	AllowAiCrawlers *bool `json:"allowAiCrawlers"`
}

// updateVerificationSection is the partial verification block.
type updateVerificationSection struct {
	Google    *string `json:"google"`
	Bing      *string `json:"bing"`
	Yandex    *string `json:"yandex"`
	Pinterest *string `json:"pinterest"`
}

// updateAnalyticsSection is the partial analytics block.
type updateAnalyticsSection struct {
	GA4ID *string `json:"ga4Id"`
	GTMID *string `json:"gtmId"`
}

// updateOrganizationSection is the partial organization block.
type updateOrganizationSection struct {
	Name         *string   `json:"name"`
	LegalName    *string   `json:"legalName"`
	Logo         *string   `json:"logo"`
	Email        *string   `json:"email"`
	Phone        *string   `json:"phone"`
	Street       *string   `json:"street"`
	Locality     *string   `json:"locality"`
	Region       *string   `json:"region"`
	PostalCode   *string   `json:"postalCode"`
	Country      *string   `json:"country"`
	SameAs       *[]string `json:"sameAs"`
	GeoStatement *string   `json:"geoStatement"`
}

// updateProfileRequest is the partial body for PUT /api/v1/seo/profile. Every
// section (and every field) is optional; only provided fields are written.
type updateProfileRequest struct {
	Site         *updateSiteSection         `json:"site"`
	Indexing     *updateIndexingSection     `json:"indexing"`
	Verification *updateVerificationSection `json:"verification"`
	Analytics    *updateAnalyticsSection    `json:"analytics"`
	Organization *updateOrganizationSection `json:"organization"`
}

// updateSEOProfile serves PUT /api/v1/seo/profile: writes only the provided
// override keys (a provided empty string clears the override), validating the
// analytics ids, then returns the full refreshed profile.
func (h *handler) updateSEOProfile(w http.ResponseWriter, r *http.Request) {
	var req updateProfileRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	// Validate analytics ids before any write (non-empty ids must be well-formed).
	if req.Analytics != nil {
		fields := map[string]string{}
		if v := req.Analytics.GA4ID; v != nil && *v != "" && !web.ValidateGA4ID(*v) {
			fields["analytics.ga4Id"] = "not a valid GA4/gtag id"
		}
		if v := req.Analytics.GTMID; v != nil && *v != "" && !web.ValidateGTMID(*v) {
			fields["analytics.gtmId"] = "not a valid GTM container id"
		}
		if len(fields) > 0 {
			FailValidation(w, fields)
			return
		}
	}

	// Collect (key,value) writes; a provided pointer writes even when empty.
	writes := map[string]string{}
	k := profileKeys
	if s := req.Site; s != nil {
		putStr(writes, k.SiteName, s.Name)
		putStr(writes, k.SiteDescription, s.Description)
		putStr(writes, k.SiteDefaultOGImage, s.DefaultOGImage)
		putStr(writes, k.SiteTwitterHandle, s.TwitterHandle)
	}
	if s := req.Indexing; s != nil {
		putBool(writes, k.SEOGlobalNoindex, s.GlobalNoindex)
		putBool(writes, k.SEOAllowAICrawlers, s.AllowAiCrawlers)
	}
	if s := req.Verification; s != nil {
		putStr(writes, k.SEOGoogleVerification, s.Google)
		putStr(writes, k.SEOBingVerification, s.Bing)
		putStr(writes, k.SEOYandexVerification, s.Yandex)
		putStr(writes, k.SEOPinterestVerification, s.Pinterest)
	}
	if s := req.Analytics; s != nil {
		putStr(writes, k.AnalyticsGA4ID, s.GA4ID)
		putStr(writes, k.AnalyticsGTMID, s.GTMID)
	}
	if s := req.Organization; s != nil {
		putStr(writes, k.OrgName, s.Name)
		putStr(writes, k.OrgLegalName, s.LegalName)
		putStr(writes, k.OrgLogo, s.Logo)
		putStr(writes, k.OrgEmail, s.Email)
		putStr(writes, k.OrgPhone, s.Phone)
		putStr(writes, k.OrgStreet, s.Street)
		putStr(writes, k.OrgLocality, s.Locality)
		putStr(writes, k.OrgRegion, s.Region)
		putStr(writes, k.OrgPostalCode, s.PostalCode)
		putStr(writes, k.OrgCountry, s.Country)
		putStr(writes, k.OrgGeoStatement, s.GeoStatement)
		if s.SameAs != nil {
			writes[k.OrgSameAs] = strings.Join(*s.SameAs, "\n")
		}
	}

	for key, val := range writes {
		if err := h.settings.Set(r.Context(), key, val); err != nil {
			Fail(w, http.StatusInternalServerError, "internal", "failed to save seo profile")
			return
		}
	}

	dto, err := h.readProfile(r.Context())
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to read seo profile")
		return
	}
	OK(w, http.StatusOK, dto)
}

// putStr records a string write when the pointer is provided (nil is skipped;
// an empty value clears the override).
func putStr(m map[string]string, key string, v *string) {
	if v != nil {
		m[key] = *v
	}
}

// putBool records a bool write ("1"/"0") when the pointer is provided.
func putBool(m map[string]string, key string, v *bool) {
	if v == nil {
		return
	}
	if *v {
		m[key] = "1"
	} else {
		m[key] = "0"
	}
}
