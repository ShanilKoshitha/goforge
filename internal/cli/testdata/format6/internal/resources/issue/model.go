package issue

import "example.com/format6/internal/models"

// Issue preserves the resource package API while the application-owned
// model and generated ORM descriptors live in internal/models.
type Issue = models.Issue
