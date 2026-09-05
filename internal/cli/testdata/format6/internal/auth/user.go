package auth

import "example.com/format6/internal/models"

// User remains source-compatible for auth callers while persistence ownership
// lives in the application's models package.
type User = models.User
