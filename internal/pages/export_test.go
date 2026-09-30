package pages

// SetSitemapMax lets a test prove the chunk boundary on its own handler without 50,000 rows.
func (h *Handler) SetSitemapMax(n int) { h.sitemapMax = n }
