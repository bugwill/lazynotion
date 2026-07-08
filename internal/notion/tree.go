package notion

// Tree surgery over cached block trees: inserting siblings, appending
// children, splicing replacements, and locating a node's placement. All
// return fresh top-level slices where the path to the change is copied.

// InsertSibling places node before/after targetID, wherever it nests.
func InsertSibling(nodes []BlockNode, targetID string, node BlockNode, before bool) ([]BlockNode, bool) {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == targetID {
			out := make([]BlockNode, 0, len(nodes)+1)
			at := i + 1
			if before {
				at = i
			}
			out = append(out, nodes[:at]...)
			out = append(out, node)
			out = append(out, nodes[at:]...)
			return out, true
		}
		if children, ok := InsertSibling(nodes[i].Children, targetID, node, before); ok {
			nodes[i].Children = children
			return nodes, true
		}
	}
	return nodes, false
}

// AppendChild adds node at the end of parentID's children.
func AppendChild(nodes []BlockNode, parentID string, node BlockNode) ([]BlockNode, bool) {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == parentID {
			nodes[i].Children = append(nodes[i].Children, node)
			return nodes, true
		}
		if children, ok := AppendChild(nodes[i].Children, parentID, node); ok {
			nodes[i].Children = children
			return nodes, true
		}
	}
	return nodes, false
}

// ReplaceNode splices replacements in place of the node with id.
func ReplaceNode(nodes []BlockNode, id string, replacements []BlockNode) ([]BlockNode, bool) {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == id {
			out := make([]BlockNode, 0, len(nodes)+len(replacements)-1)
			out = append(out, nodes[:i]...)
			out = append(out, replacements...)
			out = append(out, nodes[i+1:]...)
			return out, true
		}
		if children, ok := ReplaceNode(nodes[i].Children, id, replacements); ok {
			nodes[i].Children = children
			return nodes, true
		}
	}
	return nodes, false
}

// FindPlacement reports where id lives: its parent block ("" at page
// level) and the sibling directly before it ("" if first).
func FindPlacement(nodes []BlockNode, id string) (parentID, prevSibling string, found bool) {
	return findPlacement(nodes, id, "")
}

func findPlacement(nodes []BlockNode, id, parent string) (string, string, bool) {
	prev := ""
	for i := range nodes {
		if nodes[i].Block.GetID().String() == id {
			return parent, prev, true
		}
		if p, s, ok := findPlacement(nodes[i].Children, id, nodes[i].Block.GetID().String()); ok {
			return p, s, ok
		}
		prev = nodes[i].Block.GetID().String()
	}
	return "", "", false
}

// ChildrenOf returns the children slice of the node with id.
func ChildrenOf(nodes []BlockNode, id string) ([]BlockNode, bool) {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == id {
			return nodes[i].Children, true
		}
		if children, ok := ChildrenOf(nodes[i].Children, id); ok {
			return children, true
		}
	}
	return nil, false
}

// PatchBlockIDInTree rewrites a block's ID wherever it nests.
func PatchBlockIDInTree(nodes []BlockNode, oldID, newID string) bool {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == oldID {
			nodes[i] = ReplaceBlockID(nodes[i], newID)
			return true
		}
		if PatchBlockIDInTree(nodes[i].Children, oldID, newID) {
			return true
		}
	}
	return false
}

// FindNode returns the node with id (the copy shares the Block pointer, so
// optimistic mutations through it reach the tree).
func FindNode(nodes []BlockNode, id string) (BlockNode, bool) {
	for i := range nodes {
		if nodes[i].Block.GetID().String() == id {
			return nodes[i], true
		}
		if found, ok := FindNode(nodes[i].Children, id); ok {
			return found, ok
		}
	}
	return BlockNode{}, false
}
