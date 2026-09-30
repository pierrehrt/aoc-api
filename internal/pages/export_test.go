package pages

// SetSitemapMaxURLs lets a test prove the chunk boundary without 50,000 rows. Test-only.
func SetSitemapMaxURLs(n int) (restore func()) {
	old := sitemapMaxURLs
	sitemapMaxURLs = n
	return func() { sitemapMaxURLs = old }
}
