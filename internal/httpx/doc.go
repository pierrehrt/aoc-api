// Package httpx owns the HTTP edge: middleware, the one error->status mapping, cache
// headers, and the operational /health handler.
//
// What belongs here: anything true of every request regardless of domain.
// What does not: business rules, database access, or anything that would make a
// domain package import this one for non-HTTP reasons.
package httpx
