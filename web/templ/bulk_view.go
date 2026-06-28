package templ

import "fmt"

// BulkSummary is the post-action outcome banner surfaced on an admin list after
// a bulk operation redirects back (DESIGN_SYSTEM §5: selection state announced
// via aria-live). Present is false when no bulk action just ran.
type BulkSummary struct {
	Present bool
	Action  string // trash | restore | delete | publish | unpublish
	Applied int
	Skipped int // skipped-unauthorized (e.g. an Author's bulk touched others' posts)
	Missing int // ids with no matching row
}

// Message renders the human summary line for the aria-live banner.
func (b BulkSummary) Message() string {
	verb := map[string]string{
		"trash":     "moved to trash",
		"restore":   "restored",
		"delete":    "permanently deleted",
		"publish":   "published",
		"unpublish": "unpublished",
	}[b.Action]
	if verb == "" {
		verb = "updated"
	}
	msg := fmt.Sprintf("%d %s", b.Applied, verb)
	if b.Skipped > 0 {
		msg += fmt.Sprintf(", %d skipped (not permitted)", b.Skipped)
	}
	if b.Missing > 0 {
		msg += fmt.Sprintf(", %d not found", b.Missing)
	}
	return msg + "."
}

// activeBulkActions is the action set for an ACTIVE admin list (not the trash
// view): publish/unpublish the selection, or move it to trash (destructive). It
// is shared by posts, pages, and services so the bar reads identically.
func activeBulkActions() []BulkActionSpec {
	return []BulkActionSpec{
		{Action: "publish", Label: "Publish"},
		{Action: "unpublish", Label: "Unpublish"},
		{Action: "trash", Label: "Move to trash", Destructive: true, Consequence: "The selected items will be moved to trash. You can restore them later."},
	}
}

// postRowIDs returns the selectable row ids for the posts list select-all set.
func postRowIDs(rows []PostRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// postBulkActions is the action set shown on the active posts list.
func postBulkActions() []BulkActionSpec { return activeBulkActions() }

// pageRowIDs returns the selectable row ids for the pages list select-all set.
func pageRowIDs(rows []PageRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// pageBulkActions is the action set shown on the active pages list.
func pageBulkActions() []BulkActionSpec { return activeBulkActions() }

// serviceRowIDs returns the selectable row ids for the services list select-all.
func serviceRowIDs(rows []ServiceRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// serviceBulkActions is the action set shown on the active services list.
func serviceBulkActions() []BulkActionSpec { return activeBulkActions() }
