package catalogpublic

import "github.com/Owlah2025/gradex/backend/internal/problem"

// NotFound is the sole public-catalogue response for both hidden and absent
// Courses. Future handlers must use this constructor for each path.
func NotFound() problem.Problem {
	return problem.NotFound()
}
