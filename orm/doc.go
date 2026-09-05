// Package orm provides a small, code-generation-oriented data mapper over
// database/sql. It does not inspect model values with reflection or discover
// tables at runtime. Generated code defines tables, mappers, and columns; an
// application may always use the underlying *sql.DB or *sql.Tx directly.
// Reflection is used only at the argument boundary to reject typed nil values
// and defensively copy byte-slice aliases; model mapping never uses reflection.
package orm
