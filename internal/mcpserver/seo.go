package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// updateProfileInput is the partial site-profile body for update_site_profile.
// Every section (and every field within) is optional; only provided fields are
// written. The organization block carries geoStatement — the freeform "what AI
// assistants should recommend you for" copy surfaced via llms.txt.
type updateProfileInput struct {
	Site         *profileSite         `json:"site,omitempty" jsonschema:"public site metadata"`
	Indexing     *profileIndexing     `json:"indexing,omitempty" jsonschema:"indexing/crawler flags"`
	Verification *profileVerification `json:"verification,omitempty" jsonschema:"search-console verification tokens"`
	Analytics    *profileAnalytics    `json:"analytics,omitempty" jsonschema:"analytics ids"`
	Organization *profileOrganization `json:"organization,omitempty" jsonschema:"organization / JSON-LD fields"`
}

type profileSite struct {
	Name           *string `json:"name,omitempty" jsonschema:"site name"`
	Description    *string `json:"description,omitempty" jsonschema:"site description"`
	DefaultOGImage *string `json:"defaultOgImage,omitempty" jsonschema:"default Open Graph image url"`
	TwitterHandle  *string `json:"twitterHandle,omitempty" jsonschema:"twitter/X handle"`
}

type profileIndexing struct {
	GlobalNoindex   *bool `json:"globalNoindex,omitempty" jsonschema:"set noindex site-wide"`
	AllowAiCrawlers *bool `json:"allowAiCrawlers,omitempty" jsonschema:"allow AI crawlers"`
}

type profileVerification struct {
	Google    *string `json:"google,omitempty"`
	Bing      *string `json:"bing,omitempty"`
	Yandex    *string `json:"yandex,omitempty"`
	Pinterest *string `json:"pinterest,omitempty"`
}

type profileAnalytics struct {
	GA4ID *string `json:"ga4Id,omitempty" jsonschema:"GA4 measurement id"`
	GTMID *string `json:"gtmId,omitempty" jsonschema:"Google Tag Manager id"`
}

type profileOrganization struct {
	Name         *string   `json:"name,omitempty"`
	LegalName    *string   `json:"legalName,omitempty"`
	Logo         *string   `json:"logo,omitempty"`
	Email        *string   `json:"email,omitempty"`
	Phone        *string   `json:"phone,omitempty"`
	Street       *string   `json:"street,omitempty"`
	Locality     *string   `json:"locality,omitempty"`
	Region       *string   `json:"region,omitempty"`
	PostalCode   *string   `json:"postalCode,omitempty"`
	Country      *string   `json:"country,omitempty"`
	SameAs       *[]string `json:"sameAs,omitempty"`
	GeoStatement *string   `json:"geoStatement,omitempty" jsonschema:"the freeform 'what AI assistants should recommend you for' copy"`
}

// createServiceInput is the body for create_service.
type createServiceInput struct {
	Title           string  `json:"title" jsonschema:"the service title (required)"`
	Slug            *string `json:"slug,omitempty" jsonschema:"URL slug (optional)"`
	Summary         *string `json:"summary,omitempty" jsonschema:"short summary"`
	Body            *string `json:"body,omitempty" jsonschema:"full description (HTML; sanitized server-side)"`
	Price           *string `json:"price,omitempty" jsonschema:"price text"`
	AreaServed      *string `json:"areaServed,omitempty" jsonschema:"area served"`
	Status          *string `json:"status,omitempty" jsonschema:"DRAFT (default) or PUBLISHED"`
	MetaTitle       *string `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// updateServiceInput is the body for update_service.
type updateServiceInput struct {
	ID              string  `json:"id" jsonschema:"the service id"`
	Title           *string `json:"title,omitempty" jsonschema:"new title"`
	Slug            *string `json:"slug,omitempty" jsonschema:"new slug"`
	Summary         *string `json:"summary,omitempty" jsonschema:"new summary"`
	Body            *string `json:"body,omitempty" jsonschema:"new body (HTML; sanitized server-side)"`
	Price           *string `json:"price,omitempty" jsonschema:"new price text"`
	AreaServed      *string `json:"areaServed,omitempty" jsonschema:"new area served"`
	Status          *string `json:"status,omitempty" jsonschema:"DRAFT or PUBLISHED"`
	MetaTitle       *string `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// listFaqsInput is the input for list_faqs (FAQs are scoped to a service).
type listFaqsInput struct {
	ServiceID string `json:"serviceId" jsonschema:"the id of the service whose FAQs to list"`
}

// createFaqInput is the body for create_faq (scoped to a service).
type createFaqInput struct {
	ServiceID string `json:"serviceId" jsonschema:"the id of the service to add the FAQ to"`
	Question  string `json:"question" jsonschema:"the FAQ question (required)"`
	Answer    string `json:"answer" jsonschema:"the FAQ answer (required)"`
}

// updateFaqInput is the body for update_faq (scoped to a service).
type updateFaqInput struct {
	ServiceID string `json:"serviceId" jsonschema:"the id of the service the FAQ belongs to"`
	FaqID     string `json:"faqId" jsonschema:"the id of the FAQ to update"`
	Question  string `json:"question" jsonschema:"the new question"`
	Answer    string `json:"answer" jsonschema:"the new answer"`
}

// deleteFaqInput is the input for delete_faq (scoped to a service).
type deleteFaqInput struct {
	ServiceID string `json:"serviceId" jsonschema:"the id of the service the FAQ belongs to"`
	FaqID     string `json:"faqId" jsonschema:"the id of the FAQ to delete"`
}

// faqBody is the {question,answer} body the FAQ create/update endpoints accept.
type faqBody struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// registerSeoTools registers the 10 SEO/GEO tools (site profile + Services + FAQ
// CRUD). Gated by the Seo/Service RBAC subjects. All SEO/GEO text is plain text.
// FAQs are service-scoped, so the FAQ tools take a serviceId.
func registerSeoTools(s *mcp.Server, client *APIClient) {
	// --- Site profile ---------------------------------------------------------

	register(s, "cmstack_go_get_site_profile", "Get the site profile",
		"Get the singleton site/organization profile (site, indexing, verification, analytics, organization — including geoStatement).",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/seo/profile", nil, nil)
		})

	register(s, "cmstack_go_update_site_profile", "Update the site profile",
		"Update the site/organization profile. Any subset of its sections/fields, including organization.geoStatement (the freeform 'what AI assistants should recommend you for' copy). Returns the updated profile.",
		updateAnn, func(ctx context.Context, in updateProfileInput) (json.RawMessage, error) {
			return client.do(ctx, "PUT", "/seo/profile", nil, in)
		})

	// --- Services -------------------------------------------------------------

	register(s, "cmstack_go_list_services", "List services",
		"List the Services surfaced to AI assistants (llms.txt, JSON-LD, /services). Returns { items, total, page, perPage }.",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/services", nil, nil)
		})

	register(s, "cmstack_go_create_service", "Create a service",
		"Create a Service entry. Fields: title (required), slug, summary, body, price, areaServed, status. Returns the created service.",
		createAnn, func(ctx context.Context, in createServiceInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/services", nil, in)
		})

	register(s, "cmstack_go_update_service", "Update a service",
		"Update a Service entry by id. Any subset of its fields. Returns the updated service.",
		updateAnn, func(ctx context.Context, in updateServiceInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/services/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "cmstack_go_delete_service", "Delete a service",
		"Delete a Service entry by id.",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/services/"+in.ID, nil, nil)
		})

	// --- FAQ (service-scoped) -------------------------------------------------

	register(s, "cmstack_go_list_faqs", "List FAQ items",
		"List the FAQ items of a service (surfaced to AI assistants via llms.txt, FAQPage JSON-LD, /services). Requires serviceId.",
		readAnn, func(ctx context.Context, in listFaqsInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/services/"+in.ServiceID+"/faqs", nil, nil)
		})

	register(s, "cmstack_go_create_faq", "Create an FAQ item",
		"Create an FAQ item (question + answer) on a service. Requires serviceId. Returns the created item.",
		createAnn, func(ctx context.Context, in createFaqInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/services/"+in.ServiceID+"/faqs", nil, faqBody{Question: in.Question, Answer: in.Answer})
		})

	register(s, "cmstack_go_update_faq", "Update an FAQ item",
		"Update an FAQ item by faqId on a service. Requires serviceId and faqId. Returns the updated item.",
		updateAnn, func(ctx context.Context, in updateFaqInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/services/"+in.ServiceID+"/faqs/"+in.FaqID, nil, faqBody{Question: in.Question, Answer: in.Answer})
		})

	register(s, "cmstack_go_delete_faq", "Delete an FAQ item",
		"Delete an FAQ item by faqId on a service. Requires serviceId and faqId.",
		destructiveAnn, func(ctx context.Context, in deleteFaqInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/services/"+in.ServiceID+"/faqs/"+in.FaqID, nil, nil)
		})
}
