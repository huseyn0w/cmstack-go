package comments

import (
	"sort"

	"github.com/google/uuid"
)

// buildThread assembles a nested comment tree from a flat, chronologically
// ordered slice of public comments. A reply is attached under its parent;
// comments whose parent is not in the set (e.g. a reply whose parent was
// trashed) are promoted to the top level so they are not lost. Within each
// level the original chronological order is preserved.
//
// It is a PURE function (no I/O) so it is directly unit-testable, mirroring the
// canonical ts buildCommentThread.
func buildThread(flat []PublicComment) []PublicComment {
	// Index every node by id; copy so the caller's slice is not mutated and each
	// node gets a fresh Replies slice.
	nodes := make(map[uuid.UUID]*PublicComment, len(flat))
	order := make([]uuid.UUID, 0, len(flat))
	for i := range flat {
		c := flat[i]
		c.Replies = []PublicComment{}
		nodes[c.ID] = &c
		order = append(order, c.ID)
	}

	// Wire each node under its parent (in chronological order); the ones with no
	// in-set parent become roots.
	roots := make([]uuid.UUID, 0, len(flat))
	for _, id := range order {
		node := nodes[id]
		if node.ParentID != nil {
			if parent, ok := nodes[*node.ParentID]; ok {
				parent.Replies = append(parent.Replies, *node)
				continue
			}
		}
		roots = append(roots, id)
	}

	out := make([]PublicComment, 0, len(roots))
	for _, id := range roots {
		out = append(out, materialize(id, nodes))
	}
	return out
}

// materialize builds the subtree rooted at id, recursively resolving each reply
// from the index so nested levels are fully populated.
func materialize(id uuid.UUID, nodes map[uuid.UUID]*PublicComment) PublicComment {
	node := *nodes[id]
	replies := make([]PublicComment, 0, len(node.Replies))
	for _, child := range node.Replies {
		replies = append(replies, materialize(child.ID, nodes))
	}
	node.Replies = replies
	return node
}

// sortByCreatedAt sorts the comments oldest-first (stable on ties by id) — the
// order the public thread is presented in after merging a viewer's own pending
// comments into the approved set.
func sortByCreatedAt(cs []PublicComment) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].CreatedAt.Equal(cs[j].CreatedAt) {
			return cs[i].ID.String() < cs[j].ID.String()
		}
		return cs[i].CreatedAt.Before(cs[j].CreatedAt)
	})
}
