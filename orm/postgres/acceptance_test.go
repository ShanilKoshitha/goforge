package postgres_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	forgeDB "github.com/ShanilKoshitha/goforge/database"
	"github.com/ShanilKoshitha/goforge/orm"
	ormpostgres "github.com/ShanilKoshitha/goforge/orm/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type liveWidget struct {
	ID        int64
	Name      string
	Note      sql.NullString
	Score     int64
	Version   int64
	Slug      sql.NullString
	ParentID  sql.NullInt64
	CreatedAt time.Time
	Parent    *liveWidget
	Children  []liveWidget
	Featured  *liveWidget
	Tags      []liveTag
}

var liveWidgetMapper = orm.MustMapper([]string{
	"id", "name", "note", "score", "version", "slug", "parent_id", "created_at",
}, func(row orm.RowScanner) (liveWidget, error) {
	var item liveWidget
	err := row.Scan(
		&item.ID, &item.Name, &item.Note, &item.Score, &item.Version,
		&item.Slug, &item.ParentID, &item.CreatedAt,
	)
	return item, err
})

var (
	liveWidgetTable     = orm.MustTable("orm_live_widgets", liveWidgetMapper)
	liveWidgetID        = orm.MustColumn[liveWidget, int64](liveWidgetTable, "id")
	liveWidgetName      = orm.MustColumn[liveWidget, string](liveWidgetTable, "name")
	liveWidgetNote      = orm.MustColumn[liveWidget, string](liveWidgetTable, "note")
	liveWidgetScore     = orm.MustColumn[liveWidget, int64](liveWidgetTable, "score")
	liveWidgetVersion   = orm.MustColumn[liveWidget, int64](liveWidgetTable, "version")
	liveWidgetSlug      = orm.MustColumn[liveWidget, string](liveWidgetTable, "slug")
	liveWidgetParentID  = orm.MustColumn[liveWidget, int64](liveWidgetTable, "parent_id")
	liveWidgetParentPtr = orm.MustColumn[liveWidget, *int64](liveWidgetTable, "parent_id")
	liveWidgetCreatedAt = orm.MustColumn[liveWidget, time.Time](liveWidgetTable, "created_at")
)

type liveTag struct {
	ID   int64
	Name string
}

var liveTagMapper = orm.MustMapper([]string{"id", "name"}, func(row orm.RowScanner) (liveTag, error) {
	var item liveTag
	err := row.Scan(&item.ID, &item.Name)
	return item, err
})

var (
	liveTagTable = orm.MustTable("orm_live_tags", liveTagMapper)
	liveTagID    = orm.MustColumn[liveTag, int64](liveTagTable, "id")
	liveTagName  = orm.MustColumn[liveTag, string](liveTagTable, "name")
)

type liveDefault struct {
	ID    int64
	Label string
	Note  sql.NullString
}

var liveDefaultMapper = orm.MustMapper([]string{"id", "label", "note"}, func(row orm.RowScanner) (liveDefault, error) {
	var item liveDefault
	err := row.Scan(&item.ID, &item.Label, &item.Note)
	return item, err
})

var liveDefaultTable = orm.MustTable("orm_live_defaults", liveDefaultMapper)

func TestLivePostgresORMAcceptance(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	db := openIsolatedPostgres(t, databaseURL)
	createLiveSchema(t, db)
	ctx := context.Background()

	defaulted, err := orm.Insert(liveDefaultTable).One(ctx, db)
	if err != nil {
		t.Fatalf("insert DEFAULT VALUES: %v", err)
	}
	if defaulted.ID == 0 || defaulted.Label != "defaulted" || defaulted.Note.Valid {
		t.Fatalf("database defaults = %#v", defaulted)
	}

	created, err := orm.Insert(liveWidgetTable).Values(
		liveWidgetName.Set("typed-created"),
		liveWidgetNote.From(orm.Null[string]()),
		liveWidgetSlug.Set("typed-created"),
	).One(ctx, db)
	if err != nil {
		t.Fatalf("typed insert: %v", err)
	}
	if created.ID == 0 || created.Note.Valid || created.Score != 7 || created.Version != 1 || created.CreatedAt.IsZero() {
		t.Fatalf("inserted defaults/null/generated fields = %#v", created)
	}

	selected, err := orm.Select(liveWidgetTable).
		Where(liveWidgetName.Eq("typed-created")).
		OrderBy(liveWidgetID.Desc()).
		First(ctx, db)
	if err != nil || selected.ID != created.ID {
		t.Fatalf("typed select = %#v, %v", selected, err)
	}
	count, err := orm.Select(liveWidgetTable).Where(liveWidgetScore.Gte(7)).Count(ctx, db)
	if err != nil || count != 1 {
		t.Fatalf("typed count = %d, %v", count, err)
	}
	exists, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(created.ID)).Exists(ctx, db)
	if err != nil || !exists {
		t.Fatalf("typed exists = %v, %v", exists, err)
	}

	updated, err := orm.Update(liveWidgetTable).Set(
		liveWidgetName.Set("typed-updated"),
		liveWidgetNote.Set("present"),
		orm.Increment(liveWidgetScore, int64(2)),
		orm.Increment(liveWidgetVersion, int64(1)),
		orm.SetCurrentTime(liveWidgetCreatedAt),
	).Where(liveWidgetID.Eq(created.ID)).One(ctx, db)
	if err != nil {
		t.Fatalf("typed update RETURNING: %v", err)
	}
	if updated.Name != "typed-updated" || !updated.Note.Valid || updated.Note.String != "present" || updated.Score != 9 || updated.Version != 2 {
		t.Fatalf("updated model = %#v", updated)
	}

	testTransactionComposition(t, db)
	testBulkAndOptimisticMutations(t, db)
	testConstraintClassification(t, db, created)
	testRelationships(t, db)
	testRowLock(t, db)
	testShareLock(t, db)
	testSerializationFailure(t, db)
	testDeadlockFailure(t, db)
	testDeadlineAndPoolReuse(t, db)

	deleted, err := orm.Delete(liveWidgetTable).Where(liveWidgetID.Eq(created.ID)).Exec(ctx, db)
	if err != nil || deleted != 1 {
		t.Fatalf("typed delete = %d, %v", deleted, err)
	}
	exists, err = orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(created.ID)).Exists(ctx, db)
	if err != nil || exists {
		t.Fatalf("deleted row exists = %v, %v", exists, err)
	}
}

func openIsolatedPostgres(t *testing.T, databaseURL string) *sql.DB {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatalf("GOFORGE_TEST_DATABASE_URL must be a PostgreSQL URL: %v", err)
	}
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	setupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(setupContext); err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := fmt.Sprintf("goforge_orm_%d_%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))
	if _, err := admin.ExecContext(setupContext, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupContext, `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(6)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(setupContext); err != nil {
		t.Fatalf("connect to isolated schema: %v", err)
	}
	return db
}

func createLiveSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statements := []string{
		`CREATE TABLE orm_live_widgets (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			note TEXT NULL,
			score BIGINT NOT NULL DEFAULT 7,
			version BIGINT NOT NULL DEFAULT 1,
			slug TEXT NULL,
			parent_id BIGINT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT orm_live_widgets_slug_key UNIQUE (slug),
			CONSTRAINT orm_live_widgets_score_check CHECK (score >= 0),
			CONSTRAINT orm_live_widgets_parent_fkey FOREIGN KEY (parent_id) REFERENCES orm_live_widgets(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE orm_live_defaults (
			id BIGSERIAL PRIMARY KEY,
			label TEXT NOT NULL DEFAULT 'defaulted',
			note TEXT NULL
		)`,
		`CREATE TABLE orm_live_tags (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE orm_live_widget_tags (
			widget_id BIGINT NOT NULL REFERENCES orm_live_widgets(id) ON DELETE CASCADE,
			tag_id BIGINT NOT NULL REFERENCES orm_live_tags(id) ON DELETE CASCADE,
			PRIMARY KEY (widget_id, tag_id)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create live ORM table: %v", err)
		}
	}
}

func testTransactionComposition(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	rollbackCause := errors.New("force rollback")
	err := forgeDB.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
		if _, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("rollback-orm")).One(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO orm_live_widgets (name) VALUES ($1)`, "rollback-raw"); err != nil {
			return err
		}
		return rollbackCause
	})
	if !errors.Is(err, rollbackCause) {
		t.Fatalf("transaction rollback error = %v", err)
	}
	count, err := orm.Select(liveWidgetTable).Where(orm.Or(
		liveWidgetName.Eq("rollback-orm"), liveWidgetName.Eq("rollback-raw"),
	)).Count(ctx, db)
	if err != nil || count != 0 {
		t.Fatalf("transaction rollback left %d rows: %v", count, err)
	}

	err = forgeDB.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
		if _, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("commit-orm")).One(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO orm_live_widgets (name) VALUES ($1)`, "commit-raw")
		return err
	})
	if err != nil {
		t.Fatalf("transaction commit: %v", err)
	}
	count, err = orm.Select(liveWidgetTable).Where(orm.Or(
		liveWidgetName.Eq("commit-orm"), liveWidgetName.Eq("commit-raw"),
	)).Count(ctx, db)
	if err != nil || count != 2 {
		t.Fatalf("transaction commit rows = %d: %v", count, err)
	}
}

func testBulkAndOptimisticMutations(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	items, err := orm.Insert(liveWidgetTable).Rows(
		orm.Row(
			liveWidgetName.Set("bulk-default"),
			liveWidgetNote.From(orm.Null[string]()),
			liveWidgetScore.From(orm.Default[int64]()),
		),
		orm.Row(
			liveWidgetName.Set("bulk-value"),
			liveWidgetNote.Set("bulk-note"),
			liveWidgetScore.Set(12),
		),
	).All(ctx, db)
	if err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	if len(items) != 2 || items[0].Score != 7 || items[0].Note.Valid || items[1].Score != 12 || items[1].Note.String != "bulk-note" {
		t.Fatalf("bulk insert defaults/values = %#v", items)
	}
	_, err = orm.Insert(liveWidgetTable).Rows(
		orm.Row(liveWidgetName.Set("bulk-atomic-good"), liveWidgetSlug.Set("bulk-atomic-good")),
		orm.Row(liveWidgetName.Set("bulk-atomic-bad"), liveWidgetSlug.Set("typed-created")),
	).All(ctx, db)
	if !errors.Is(ormpostgres.Classify(err), orm.ErrUnique) {
		t.Fatalf("bulk atomic constraint classification = %v", err)
	}
	count, err := orm.Select(liveWidgetTable).Where(liveWidgetName.Eq("bulk-atomic-good")).Count(ctx, db)
	if err != nil || count != 0 {
		t.Fatalf("failed bulk insert persisted %d rows: %v", count, err)
	}

	versioned, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("version-one")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	versioned, err = orm.Update(liveWidgetTable).
		Set(liveWidgetName.Set("version-two")).
		Where(liveWidgetID.Eq(versioned.ID)).
		OptimisticVersion(orm.ExpectVersion(liveWidgetVersion, versioned.Version)).
		StaleOnZero().
		One(ctx, db)
	if err != nil || versioned.Version != 2 || versioned.Name != "version-two" {
		t.Fatalf("optimistic success = %#v, %v", versioned, err)
	}
	_, err = orm.Update(liveWidgetTable).
		Set(liveWidgetName.Set("stale-write")).
		Where(liveWidgetID.Eq(versioned.ID)).
		OptimisticVersion(orm.ExpectVersion(liveWidgetVersion, int64(1))).
		StaleOnZero().
		One(ctx, db)
	if !errors.Is(err, orm.ErrStale) {
		t.Fatalf("optimistic stale error = %v", err)
	}

	owner, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("optimistic-owner")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := orm.Insert(liveWidgetTable).Values(
		liveWidgetName.Set("owner-scoped"), liveWidgetParentID.Set(owner.ID),
	).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = orm.Update(liveWidgetTable).
		Set(liveWidgetName.Set("wrong-owner-write")).
		Where(liveWidgetID.Eq(owned.ID), liveWidgetParentID.Eq(owner.ID+1)).
		OptimisticVersion(orm.ExpectVersion(liveWidgetVersion, owned.Version)).
		One(ctx, db)
	if !errors.Is(err, orm.ErrNotFound) || errors.Is(err, orm.ErrStale) {
		t.Fatalf("wrong owner must remain not-found, got %v", err)
	}
	unchanged, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(owned.ID)).First(ctx, db)
	if err != nil || unchanged.Name != "owner-scoped" || unchanged.Version != owned.Version {
		t.Fatalf("wrong-owner mutation changed row = %#v, %v", unchanged, err)
	}
}

func testConstraintClassification(t *testing.T, db *sql.DB, existing liveWidget) {
	t.Helper()
	ctx := context.Background()
	tests := []struct {
		name       string
		build      orm.InsertBuilder[liveWidget]
		target     error
		kind       orm.ConstraintKind
		constraint string
		sensitive  string
	}{
		{
			name: "unique", target: orm.ErrUnique, kind: orm.ConstraintUnique, constraint: "orm_live_widgets_slug_key", sensitive: "typed-created",
			build: orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("duplicate"), liveWidgetSlug.Set(existing.Slug.String)),
		},
		{
			name: "foreign key", target: orm.ErrForeignKey, kind: orm.ConstraintForeignKey, constraint: "orm_live_widgets_parent_fkey", sensitive: "bad-parent",
			build: orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("bad-parent"), liveWidgetParentID.Set(9_223_372_036_854_775_000)),
		},
		{
			name: "not null", target: orm.ErrNotNull, kind: orm.ConstraintNotNull, constraint: "name",
			build: orm.Insert(liveWidgetTable).Values(liveWidgetName.From(orm.Null[string]())),
		},
		{
			name: "check", target: orm.ErrCheck, kind: orm.ConstraintCheck, constraint: "orm_live_widgets_score_check", sensitive: "bad-score",
			build: orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("bad-score"), liveWidgetScore.Set(-1)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.build.One(ctx, db)
			if err == nil {
				t.Fatal("constraint violation succeeded")
			}
			classified := ormpostgres.Classify(err)
			if !errors.Is(classified, test.target) {
				t.Fatalf("classification = %v", classified)
			}
			var persistence *orm.PersistenceError
			var constraint *orm.ConstraintError
			var postgresError *pgconn.PgError
			if !errors.As(classified, &persistence) || persistence.Operation != orm.OperationInsert || persistence.Table != liveWidgetTable.Name() {
				t.Fatalf("persistence context = %#v", persistence)
			}
			if !errors.As(classified, &constraint) || constraint.Kind != test.kind || constraint.Constraint != test.constraint {
				t.Fatalf("constraint context = %#v", constraint)
			}
			if !errors.As(classified, &postgresError) || postgresError.Code != constraint.Code {
				t.Fatalf("native PostgreSQL cause = %#v", postgresError)
			}
			if test.sensitive != "" && strings.Contains(classified.Error(), test.sensitive) {
				t.Fatalf("classified error disclosed submitted value: %v", classified)
			}
		})
	}
}

func testRelationships(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	parents := make([]liveWidget, 3)
	for index := range parents {
		parent, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set(fmt.Sprintf("parent-%d", index))).One(ctx, db)
		if err != nil {
			t.Fatalf("insert relation parent: %v", err)
		}
		parents[index] = parent
		if _, err := orm.Insert(liveWidgetTable).Values(
			liveWidgetName.Set(fmt.Sprintf("child-%d", index)),
			liveWidgetParentID.Set(parent.ID),
		).One(ctx, db); err != nil {
			t.Fatalf("insert relation child: %v", err)
		}
	}
	relation, err := orm.NewHasMany(
		liveWidgetTable, liveWidgetTable, liveWidgetID, liveWidgetParentID,
		func(item liveWidget) (int64, bool) { return item.ID, item.ID != 0 },
		func(item liveWidget) (int64, bool) { return item.ParentID.Int64, item.ParentID.Valid },
		func(item *liveWidget, children []liveWidget) { item.Children = children },
		orm.RelationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	observed := orm.ObserveExecutor(db, func(_ context.Context, event orm.StatementEvent, _ orm.Statement) {
		if event == orm.StatementQuery {
			queries.Add(1)
		}
	})
	loaded, err := relation.Load(ctx, observed, parents,
		orm.Select(liveWidgetTable).Where(liveWidgetScore.Gte(0)).OrderBy(liveWidgetID.Asc()))
	if err != nil {
		t.Fatalf("load has-many: %v", err)
	}
	if queries.Load() != 1 {
		t.Fatalf("three-parent eager load issued %d queries, want 1", queries.Load())
	}
	for index, parent := range loaded {
		if len(parent.Children) != 1 || parent.Children[0].Name != fmt.Sprintf("child-%d", index) {
			t.Fatalf("loaded parent %d children = %#v", index, parent.Children)
		}
	}

	belongsTo, err := orm.NewBelongsTo(
		liveWidgetTable, liveWidgetTable, liveWidgetParentPtr, liveWidgetID,
		func(item liveWidget) (int64, bool) { return item.ParentID.Int64, item.ParentID.Valid },
		func(item liveWidget) (int64, bool) { return item.ID, item.ID != 0 },
		func(item *liveWidget, parent *liveWidget) { item.Parent = parent },
		orm.RelationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	child, err := orm.Select(liveWidgetTable).Where(liveWidgetName.Eq("child-0")).First(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	queries.Store(0)
	children, err := belongsTo.Load(ctx, observed, []liveWidget{child, parents[0]}, orm.Select(liveWidgetTable))
	if err != nil || queries.Load() != 1 || children[0].Parent == nil || children[0].Parent.ID != parents[0].ID || children[1].Parent != nil {
		t.Fatalf("belongs-to load = %#v, %v", children, err)
	}

	hasOne, err := orm.NewHasOne(
		liveWidgetTable, liveWidgetTable, liveWidgetID, liveWidgetParentPtr,
		func(item liveWidget) (int64, bool) { return item.ID, item.ID != 0 },
		func(item liveWidget) (int64, bool) { return item.ParentID.Int64, item.ParentID.Valid },
		func(item *liveWidget, featured *liveWidget) { item.Featured = featured },
		orm.RelationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	queries.Store(0)
	one, err := hasOne.Load(ctx, observed, parents, orm.Select(liveWidgetTable).Where(orm.Like(liveWidgetName, "child-%")))
	if err != nil || queries.Load() != 1 || one[0].Featured == nil || one[0].Featured.Name != "child-0" || one[2].Featured == nil {
		t.Fatalf("has-one load = %#v, %v", one, err)
	}

	tag, err := orm.Insert(liveTagTable).Values(liveTagName.Set("orm-live-tag")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	many, err := orm.NewManyToMany(
		liveWidgetTable, liveTagTable, liveWidgetID, liveTagID,
		"orm_live_widget_tags", "widget_id", "tag_id",
		func(item liveWidget) (int64, bool) { return item.ID, item.ID != 0 },
		func(item liveTag) (int64, bool) { return item.ID, item.ID != 0 },
		func(row orm.RowScanner) (int64, liveTag, error) {
			var parentID int64
			var item liveTag
			err := row.Scan(&parentID, &item.ID, &item.Name)
			return parentID, item, err
		},
		func(item *liveWidget, tags []liveTag) { item.Tags = tags },
		orm.RelationOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := many.Attach(ctx, db, parents[0].ID, tag.ID)
	if err != nil || !attached {
		t.Fatalf("many-to-many attach = %v, %v", attached, err)
	}
	attached, err = many.Attach(ctx, db, parents[0].ID, tag.ID)
	if err != nil || attached {
		t.Fatalf("duplicate attach = %v, %v", attached, err)
	}
	queries.Store(0)
	withTags, err := many.Load(ctx, observed, parents[:2], orm.Select(liveTagTable).Where(liveTagName.Eq("orm-live-tag")))
	if err != nil || queries.Load() != 1 || len(withTags[0].Tags) != 1 || withTags[0].Tags[0].ID != tag.ID || len(withTags[1].Tags) != 0 {
		t.Fatalf("many-to-many load = %#v, %v", withTags, err)
	}
	detached, err := many.Detach(ctx, db, parents[0].ID, tag.ID)
	if err != nil || !detached {
		t.Fatalf("many-to-many detach = %v, %v", detached, err)
	}
}

func testRowLock(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	item, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("lock-before")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	if _, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(item.ID)).ForUpdate().First(ctx, first); err != nil {
		t.Fatalf("acquire row lock: %v", err)
	}
	second, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback() }()
	secondPID := transactionBackendPID(t, second)

	done := make(chan error, 1)
	go func() {
		waitContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := second.ExecContext(waitContext, `UPDATE orm_live_widgets SET name = $1 WHERE id = $2`, "lock-after", item.ID)
		done <- err
	}()
	waitForBackendLock(t, db, secondPID)
	if err := first.Commit(); err != nil {
		t.Fatalf("release row lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("competing update after unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("competing update remained blocked after commit")
	}
	if err := second.Commit(); err != nil {
		t.Fatalf("commit competing update: %v", err)
	}
	updated, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(item.ID)).First(ctx, db)
	if err != nil || updated.Name != "lock-after" {
		t.Fatalf("row-lock update result = %#v, %v", updated, err)
	}
}

func testShareLock(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	item, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("share-before")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Rollback() }()
	if _, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(item.ID)).ForShare().First(ctx, reader); err != nil {
		t.Fatalf("acquire share lock: %v", err)
	}
	writer, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback() }()
	writerPID := transactionBackendPID(t, writer)
	done := make(chan error, 1)
	go func() {
		waitContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, updateErr := orm.Update(liveWidgetTable).
			Set(liveWidgetName.Set("share-after")).
			Where(liveWidgetID.Eq(item.ID)).
			Exec(waitContext, writer)
		done <- updateErr
	}()
	waitForBackendLock(t, db, writerPID)
	if err := reader.Commit(); err != nil {
		t.Fatalf("release share lock: %v", err)
	}
	select {
	case updateErr := <-done:
		if updateErr != nil {
			t.Fatalf("update after share unlock: %v", updateErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("update remained blocked after share lock commit")
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit share-lock writer: %v", err)
	}
}

func testSerializationFailure(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	item, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("serialization")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	options := &sql.TxOptions{Isolation: sql.LevelSerializable}
	first, err := db.BeginTx(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	second, err := db.BeginTx(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback() }()
	for _, tx := range []*sql.Tx{first, second} {
		if _, err := orm.Select(liveWidgetTable).Where(liveWidgetID.Eq(item.ID)).First(ctx, tx); err != nil {
			t.Fatalf("serializable read: %v", err)
		}
	}
	if _, err := orm.Update(liveWidgetTable).Set(orm.Increment(liveWidgetScore, int64(1))).Where(liveWidgetID.Eq(item.ID)).Exec(ctx, first); err != nil {
		t.Fatalf("first serializable update: %v", err)
	}
	if err := first.Commit(); err != nil {
		t.Fatalf("first serializable commit: %v", err)
	}
	_, err = orm.Update(liveWidgetTable).Set(orm.Increment(liveWidgetScore, int64(1))).Where(liveWidgetID.Eq(item.ID)).Exec(ctx, second)
	if err == nil {
		err = second.Commit()
	}
	classified := ormpostgres.Classify(err)
	if !errors.Is(classified, orm.ErrSerialization) {
		t.Fatalf("serialization classification = %v", classified)
	}
	var postgresError *pgconn.PgError
	if !errors.As(classified, &postgresError) || postgresError.Code != "40001" {
		t.Fatalf("serialization native cause = %#v", postgresError)
	}
	assertPoolReusable(t, db)
}

func testDeadlockFailure(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	firstRow, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("deadlock-first")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	secondRow, err := orm.Insert(liveWidgetTable).Values(liveWidgetName.Set("deadlock-second")).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	second, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback() }()
	if _, err := orm.Update(liveWidgetTable).Set(liveWidgetName.Set("deadlock-first-locked")).Where(liveWidgetID.Eq(firstRow.ID)).Exec(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Update(liveWidgetTable).Set(liveWidgetName.Set("deadlock-second-locked")).Where(liveWidgetID.Eq(secondRow.ID)).Exec(ctx, second); err != nil {
		t.Fatal(err)
	}
	firstPID := transactionBackendPID(t, first)
	firstResult := make(chan error, 1)
	go func() {
		waitContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, updateErr := orm.Update(liveWidgetTable).
			Set(liveWidgetName.Set("first-waits-on-second")).
			Where(liveWidgetID.Eq(secondRow.ID)).
			Exec(waitContext, first)
		firstResult <- updateErr
	}()
	waitForBackendLock(t, db, firstPID)
	secondContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, secondErr := orm.Update(liveWidgetTable).
		Set(liveWidgetName.Set("second-waits-on-first")).
		Where(liveWidgetID.Eq(firstRow.ID)).
		Exec(secondContext, second)
	var firstErr error
	select {
	case firstErr = <-firstResult:
	case <-time.After(9 * time.Second):
		t.Fatal("deadlock survivor did not return")
	}
	firstClassified := ormpostgres.Classify(firstErr)
	secondClassified := ormpostgres.Classify(secondErr)
	firstDeadlocked := errors.Is(firstClassified, orm.ErrDeadlock)
	secondDeadlocked := errors.Is(secondClassified, orm.ErrDeadlock)
	if firstDeadlocked == secondDeadlocked {
		t.Fatalf("deadlock errors = first:%v second:%v", firstErr, secondErr)
	}
	var postgresError *pgconn.PgError
	victim := firstClassified
	if secondDeadlocked {
		victim = secondClassified
	}
	if !errors.As(victim, &postgresError) || postgresError.Code != "40P01" {
		t.Fatalf("deadlock native cause = %#v", postgresError)
	}
	if firstDeadlocked {
		if err := second.Commit(); err != nil {
			t.Fatalf("commit deadlock survivor: %v", err)
		}
	} else if err := first.Commit(); err != nil {
		t.Fatalf("commit deadlock survivor: %v", err)
	}
	assertPoolReusable(t, db)
}

func transactionBackendPID(t *testing.T, tx *sql.Tx) int {
	t.Helper()
	var pid int
	if err := tx.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read transaction backend PID: %v", err)
	}
	return pid
}

func waitForBackendLock(t *testing.T, db *sql.DB, pid int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var waiting bool
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(wait_event_type = 'Lock', false)
			FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waiting)
		if err == nil && waiting {
			return
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("inspect PostgreSQL lock wait: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d did not enter a PostgreSQL lock wait", pid)
}

func assertPoolReusable(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("pool is not reusable: value=%d err=%v", one, err)
	}
}

func testDeadlineAndPoolReuse(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var ignored any
	err := db.QueryRowContext(ctx, "SELECT pg_sleep(5)").Scan(&ignored)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if classified := ormpostgres.Classify(err); classified != err {
		t.Fatalf("classifier replaced deadline error: %v", classified)
	}
	reuseContext, reuseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer reuseCancel()
	var value int
	if err := db.QueryRowContext(reuseContext, "SELECT 1").Scan(&value); err != nil || value != 1 {
		t.Fatalf("pool was not reusable after cancellation: value=%d err=%v", value, err)
	}
}
