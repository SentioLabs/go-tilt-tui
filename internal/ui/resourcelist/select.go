package resourcelist

// Select moves the cursor to the resource called name and expands its
// group. A resource the filter hides stays listed, dimmed, until the
// cursor leaves it. Select returns false, and changes nothing, when no
// resource has that name.
func (m *Model) Select(name string) bool {
	for _, g := range m.groups {
		for _, i := range g.members {
			r := m.resources[i]
			if r.Name() != name {
				continue
			}
			delete(m.collapsed, g.name)
			m.target = target{kind: rowResource, name: name}
			m.free = false
			if !m.visible(r) {
				m.pinned = name
			}
			m.rebuild()
			return true
		}
	}
	return false
}
