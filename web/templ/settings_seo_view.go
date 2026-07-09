package templ

// SettingsSEOView is the view-model for the SEO & GEO settings dashboard. Each
// field carries the current effective value (override || config default) for
// pre-filling; Error renders an inline banner; Saved renders a success banner.
type SettingsSEOView struct {
	Shell     AdminShell
	SaveURL   string
	CSRFToken string

	// Indexing.
	GlobalNoindex   bool
	AllowAICrawlers bool

	// Search-engine verification tokens.
	GoogleVerification    string
	BingVerification      string
	YandexVerification    string
	PinterestVerification string

	// Analytics (M15-1 keys).
	GA4ID string
	GTMID string

	// Organization (JSON-LD / GEO).
	OrgName         string
	OrgLegalName    string
	OrgLogo         string
	OrgEmail        string
	OrgPhone        string
	OrgStreet       string
	OrgLocality     string
	OrgRegion       string
	OrgPostalCode   string
	OrgCountry      string
	OrgSameAs       string // newline-separated, one URL per line
	OrgGeoStatement string

	Error string
	Saved bool
}
