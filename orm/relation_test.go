package orm_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ShanilKoshitha/goforge/orm"
)

type relationAuthor struct {
	ID      int64
	Tenant  int64
	Name    string
	Profile *relationProfile
}

type relationProfile struct {
	ID       int64
	AuthorID int64
	Bio      string
}

type relationArticle struct {
	ID       int64
	AuthorID int64
	Author   *relationAuthor
	Comments []relationComment
	Tags     []relationTag
}

type relationComment struct {
	ID        int64
	ArticleID int64
	Body      string
}

type relationTag struct {
	ID     int64
	Tenant int64
	Name   string
}

func scanRelationAuthor(row orm.RowScanner) (relationAuthor, error) {
	var value relationAuthor
	err := row.Scan(&value.ID, &value.Tenant, &value.Name)
	return value, err
}

func scanRelationProfile(row orm.RowScanner) (relationProfile, error) {
	var value relationProfile
	err := row.Scan(&value.ID, &value.AuthorID, &value.Bio)
	return value, err
}

func scanRelationArticle(row orm.RowScanner) (relationArticle, error) {
	var value relationArticle
	err := row.Scan(&value.ID, &value.AuthorID)
	return value, err
}

func scanRelationComment(row orm.RowScanner) (relationComment, error) {
	var value relationComment
	err := row.Scan(&value.ID, &value.ArticleID, &value.Body)
	return value, err
}

func scanRelationTag(row orm.RowScanner) (relationTag, error) {
	var value relationTag
	err := row.Scan(&value.ID, &value.Tenant, &value.Name)
	return value, err
}

var (
	relationAuthorMapper = orm.MustMapper([]string{"id", "tenant_id", "name"}, scanRelationAuthor)
	relationAuthorTable  = orm.MustTable("relation_authors", relationAuthorMapper)
	relationAuthorID     = orm.MustColumn[relationAuthor, int64](relationAuthorTable, "id")
	relationAuthorTenant = orm.MustColumn[relationAuthor, int64](relationAuthorTable, "tenant_id")
	relationAuthorName   = orm.MustColumn[relationAuthor, string](relationAuthorTable, "name")

	relationProfileMapper   = orm.MustMapper([]string{"id", "author_id", "bio"}, scanRelationProfile)
	relationProfileTable    = orm.MustTable("relation_profiles", relationProfileMapper)
	relationProfileAuthorID = orm.MustColumn[relationProfile, int64](relationProfileTable, "author_id")

	relationArticleMapper   = orm.MustMapper([]string{"id", "author_id"}, scanRelationArticle)
	relationArticleTable    = orm.MustTable("relation_articles", relationArticleMapper)
	relationArticleID       = orm.MustColumn[relationArticle, int64](relationArticleTable, "id")
	relationArticleAuthorID = orm.MustColumn[relationArticle, int64](relationArticleTable, "author_id")

	relationCommentMapper    = orm.MustMapper([]string{"id", "article_id", "body"}, scanRelationComment)
	relationCommentTable     = orm.MustTable("relation_comments", relationCommentMapper)
	relationCommentID        = orm.MustColumn[relationComment, int64](relationCommentTable, "id")
	relationCommentArticleID = orm.MustColumn[relationComment, int64](relationCommentTable, "article_id")

	relationTagMapper = orm.MustMapper([]string{"id", "tenant_id", "name"}, scanRelationTag)
	relationTagTable  = orm.MustTable("relation_tags", relationTagMapper)
	relationTagID     = orm.MustColumn[relationTag, int64](relationTagTable, "id")
	relationTagTenant = orm.MustColumn[relationTag, int64](relationTagTable, "tenant_id")
	relationTagName   = orm.MustColumn[relationTag, string](relationTagTable, "name")
)

func belongsToAuthor(options orm.RelationOptions) orm.BelongsTo[relationArticle, relationAuthor, int64] {
	return orm.MustBelongsTo(
		relationArticleTable, relationAuthorTable, relationArticleAuthorID, relationAuthorID,
		func(article relationArticle) (int64, bool) { return article.AuthorID, article.AuthorID != 0 },
		func(author relationAuthor) (int64, bool) { return author.ID, true },
		func(article *relationArticle, author *relationAuthor) { article.Author = author },
		options,
	)
}

func authorHasOneProfile(options orm.RelationOptions) orm.HasOne[relationAuthor, relationProfile, int64] {
	return orm.MustHasOne(
		relationAuthorTable, relationProfileTable, relationAuthorID, relationProfileAuthorID,
		func(author relationAuthor) (int64, bool) { return author.ID, author.ID != 0 },
		func(profile relationProfile) (int64, bool) { return profile.AuthorID, profile.AuthorID != 0 },
		func(author *relationAuthor, profile *relationProfile) { author.Profile = profile },
		options,
	)
}

func articleHasManyComments(options orm.RelationOptions) orm.HasMany[relationArticle, relationComment, int64] {
	return orm.MustHasMany(
		relationArticleTable, relationCommentTable, relationArticleID, relationCommentArticleID,
		func(article relationArticle) (int64, bool) { return article.ID, article.ID != 0 },
		func(comment relationComment) (int64, bool) { return comment.ArticleID, comment.ArticleID != 0 },
		func(article *relationArticle, comments []relationComment) { article.Comments = comments },
		options,
	)
}

func articleManyTags(options orm.RelationOptions) orm.ManyToMany[relationArticle, relationTag, int64, int64] {
	return orm.MustManyToMany(
		relationArticleTable, relationTagTable, relationArticleID, relationTagID,
		"relation_article_tags", "article_id", "tag_id",
		func(article relationArticle) (int64, bool) { return article.ID, article.ID != 0 },
		func(tag relationTag) (int64, bool) { return tag.ID, tag.ID != 0 },
		func(row orm.RowScanner) (int64, relationTag, error) {
			var parentID int64
			var tag relationTag
			err := row.Scan(&parentID, &tag.ID, &tag.Tenant, &tag.Name)
			return parentID, tag, err
		},
		func(article *relationArticle, tags []relationTag) { article.Tags = tags },
		options,
	)
}

func TestBelongsToLoadsOneScopedQueryForOneHundredParents(t *testing.T) {
	parents := make([]relationArticle, 100)
	for index := range parents {
		parents[index] = relationArticle{ID: int64(index + 1), AuthorID: int64(index%3 + 1)}
	}
	parents[99].AuthorID = 0
	scope := orm.Select(relationAuthorTable).
		Where(relationAuthorTenant.Eq(44)).
		OrderBy(relationAuthorName.Asc())
	relation := belongsToAuthor(orm.RelationOptions{})
	statements, err := relation.Build(parents, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 {
		t.Fatalf("statements = %d, want one", len(statements))
	}
	if !strings.Contains(statements[0].SQL(), `"relation_authors"."tenant_id" = $1`) ||
		!strings.Contains(statements[0].SQL(), `"relation_authors"."id" IN ($2, $3, $4)`) {
		t.Fatalf("tenant/key scope missing: %s", statements[0].SQL())
	}
	if !reflect.DeepEqual(statements[0].Args(), []any{int64(44), int64(1), int64(2), int64(3)}) {
		t.Fatalf("deduplicated args = %#v", statements[0].Args())
	}

	script := &driverScript{
		columns: []string{"id", "tenant_id", "name"},
		rows: [][]driver.Value{
			{int64(1), int64(44), "Ada"},
			{int64(2), int64(44), "Grace"},
		},
	}
	db := openScriptedDB(t, script)
	var queries atomic.Int64
	executor := orm.ObserveExecutor(db, func(_ context.Context, event orm.StatementEvent, _ orm.Statement) {
		if event == orm.StatementQuery {
			queries.Add(1)
		}
	})
	loaded, err := relation.Load(context.Background(), executor, parents, scope)
	if err != nil {
		t.Fatal(err)
	}
	if queries.Load() != 1 {
		t.Fatalf("100 parents issued %d relation queries", queries.Load())
	}
	if parents[0].Author != nil {
		t.Fatal("loader mutated caller-owned parent slice")
	}
	if loaded[0].Author == nil || loaded[0].Author.Name != "Ada" || loaded[1].Author == nil || loaded[1].Author.Name != "Grace" {
		t.Fatalf("loaded authors = %#v / %#v", loaded[0].Author, loaded[1].Author)
	}
	if loaded[2].Author != nil || loaded[99].Author != nil {
		t.Fatal("missing or absent relation was not assigned nil")
	}
}

func TestBelongsToNormalizesNullableForeignKeyColumn(t *testing.T) {
	nullableAuthorID := orm.MustColumn[relationArticle, *int64](relationArticleTable, "author_id")
	relation := orm.MustBelongsTo(
		relationArticleTable, relationAuthorTable, nullableAuthorID, relationAuthorID,
		func(article relationArticle) (int64, bool) { return article.AuthorID, article.AuthorID != 0 },
		func(author relationAuthor) (int64, bool) { return author.ID, author.ID != 0 },
		func(article *relationArticle, author *relationAuthor) { article.Author = author },
		orm.RelationOptions{},
	)
	statements, err := relation.Build(
		[]relationArticle{{AuthorID: 7}, {AuthorID: 0}, {AuthorID: 7}},
		orm.Select(relationAuthorTable),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 ||
		!strings.HasSuffix(statements[0].SQL(), ` WHERE "relation_authors"."id" IN ($1)`) ||
		!reflect.DeepEqual(statements[0].Args(), []any{int64(7)}) {
		t.Fatalf("normalized nullable relation = %q %#v", statements[0].SQL(), statements[0].Args())
	}
}

func TestHasOneAndHasManyAssignMissingAndOrderedRelations(t *testing.T) {
	authors := []relationAuthor{{ID: 1}, {ID: 2}}
	profileScript := &driverScript{
		columns: []string{"id", "author_id", "bio"},
		rows:    [][]driver.Value{{int64(7), int64(1), "hello"}},
	}
	loadedAuthors, err := authorHasOneProfile(orm.RelationOptions{}).Load(
		context.Background(), openScriptedDB(t, profileScript), authors, orm.Select(relationProfileTable),
	)
	if err != nil {
		t.Fatal(err)
	}
	if loadedAuthors[0].Profile == nil || loadedAuthors[0].Profile.Bio != "hello" || loadedAuthors[1].Profile != nil {
		t.Fatalf("profiles = %#v", loadedAuthors)
	}

	duplicateScript := &driverScript{
		columns: []string{"id", "author_id", "bio"},
		rows: [][]driver.Value{
			{int64(7), int64(1), "first"},
			{int64(8), int64(1), "second"},
		},
	}
	if _, err := authorHasOneProfile(orm.RelationOptions{}).Load(
		context.Background(), openScriptedDB(t, duplicateScript), authors, orm.Select(relationProfileTable),
	); err == nil {
		t.Fatal("has-one accepted multiple targets")
	}

	articles := []relationArticle{{ID: 1}, {ID: 2}, {ID: 3}}
	commentScript := &driverScript{
		columns: []string{"id", "article_id", "body"},
		rows: [][]driver.Value{
			{int64(3), int64(1), "third"},
			{int64(1), int64(1), "first"},
			{int64(2), int64(2), "second"},
		},
	}
	loadedArticles, err := articleHasManyComments(orm.RelationOptions{}).Load(
		context.Background(), openScriptedDB(t, commentScript), articles,
		orm.Select(relationCommentTable).OrderBy(relationCommentID.Desc()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := []int64{loadedArticles[0].Comments[0].ID, loadedArticles[0].Comments[1].ID}; !reflect.DeepEqual(got, []int64{3, 1}) {
		t.Fatalf("child order = %v", got)
	}
	if len(loadedArticles[1].Comments) != 1 || len(loadedArticles[2].Comments) != 0 {
		t.Fatalf("child grouping = %#v", loadedArticles)
	}
}

func TestManyToManyBuildLoadDeduplicateAndAttachDetach(t *testing.T) {
	parents := []relationArticle{{ID: 1}, {ID: 1}, {ID: 2}, {ID: 3}}
	scope := orm.Select(relationTagTable).
		Where(relationTagTenant.Eq(9)).
		OrderBy(relationTagName.Asc())
	relation := articleManyTags(orm.RelationOptions{})
	statements, err := relation.Build(parents, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 || !strings.Contains(statements[0].SQL(), `JOIN "relation_article_tags"`) ||
		!strings.Contains(statements[0].SQL(), `"relation_tags"."tenant_id" = $1`) ||
		!reflect.DeepEqual(statements[0].Args(), []any{int64(9), int64(1), int64(2), int64(3)}) {
		t.Fatalf("many-to-many statement = %q %#v", statements[0].SQL(), statements[0].Args())
	}
	script := &driverScript{
		columns: []string{"article_id", "id", "tenant_id", "name"},
		rows: [][]driver.Value{
			{int64(1), int64(2), int64(9), "Beta"},
			{int64(1), int64(1), int64(9), "Alpha"},
			{int64(1), int64(1), int64(9), "Alpha duplicate"},
			{int64(2), int64(3), int64(9), "Gamma"},
		},
	}
	loaded, err := relation.Load(context.Background(), openScriptedDB(t, script), parents, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[0].Tags) != 2 || loaded[0].Tags[0].ID != 2 || loaded[0].Tags[1].ID != 1 {
		t.Fatalf("many-to-many order/dedup = %#v", loaded[0].Tags)
	}
	if !reflect.DeepEqual(loaded[0].Tags, loaded[1].Tags) || len(loaded[2].Tags) != 1 || len(loaded[3].Tags) != 0 {
		t.Fatalf("many-to-many grouping = %#v", loaded)
	}

	attach, err := relation.BuildAttach(4, 8)
	if err != nil {
		t.Fatalf("build attach: %v", err)
	}
	if attach.SQL() != `INSERT INTO "relation_article_tags" ("article_id", "tag_id") VALUES ($1, $2) ON CONFLICT ("article_id", "tag_id") DO NOTHING` ||
		!reflect.DeepEqual(attach.Args(), []any{int64(4), int64(8)}) {
		t.Fatalf("attach = %q %#v", attach.SQL(), attach.Args())
	}
	attached, err := relation.Attach(context.Background(), openScriptedDB(t, &driverScript{rowsAffected: 0}), 4, 8)
	if err != nil || attached {
		t.Fatalf("duplicate attach = %v, %v", attached, err)
	}
	detach, err := relation.BuildDetach(4, 8)
	if err != nil {
		t.Fatalf("build detach: %v", err)
	}
	if detach.SQL() != `DELETE FROM "relation_article_tags" WHERE "article_id" = $1 AND "tag_id" = $2` ||
		!reflect.DeepEqual(detach.Args(), []any{int64(4), int64(8)}) {
		t.Fatalf("detach is not pair-scoped: %q %#v", detach.SQL(), detach.Args())
	}
	detached, err := relation.Detach(context.Background(), openScriptedDB(t, &driverScript{rowsAffected: 1}), 4, 8)
	if err != nil || !detached {
		t.Fatalf("detach = %v, %v", detached, err)
	}
}

func TestRelationBatchingScopeAndCancellation(t *testing.T) {
	parents := []relationArticle{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	scope := orm.Select(relationCommentTable).Where(relationCommentID.Gt(10))
	relation := articleHasManyComments(orm.RelationOptions{ParameterLimit: 3})
	statements, err := relation.Build(parents, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("batched statements = %d, want 3", len(statements))
	}
	for _, statement := range statements {
		if len(statement.Args()) > 3 || statement.Args()[0] != int64(10) {
			t.Fatalf("batch exceeded budget or dropped scope: %#v", statement.Args())
		}
	}
	script := &driverScript{columns: []string{"id", "article_id", "body"}}
	var queries atomic.Int64
	executor := orm.ObserveExecutor(openScriptedDB(t, script), func(_ context.Context, event orm.StatementEvent, _ orm.Statement) {
		if event == orm.StatementQuery {
			queries.Add(1)
		}
	})
	if _, err := relation.Load(context.Background(), executor, parents, scope); err != nil {
		t.Fatal(err)
	}
	if queries.Load() != 3 {
		t.Fatalf("batch query count = %d", queries.Load())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	queries.Store(0)
	_, err = relation.Load(canceled, executor, parents, scope)
	if !errors.Is(err, context.Canceled) || queries.Load() != 0 {
		t.Fatalf("canceled loader = %v, queries=%d", err, queries.Load())
	}

	if _, err := relation.Build(parents, scope.Limit(1)); err == nil {
		t.Fatal("relation target scope accepted pagination")
	}
	if _, err := relation.Build(parents, scope.ForUpdate()); err == nil {
		t.Fatal("relation target scope accepted a row lock")
	}
	exhausted := articleHasManyComments(orm.RelationOptions{ParameterLimit: 1})
	if _, err := exhausted.Build(parents, scope); err == nil {
		t.Fatal("scope exhausting parameter budget succeeded")
	}
}

func TestEmptyRelationKeysDoNotPerformIO(t *testing.T) {
	parents := []relationArticle{{ID: 0}, {ID: 0}}
	script := &driverScript{columns: []string{"id", "article_id", "body"}}
	loaded, err := articleHasManyComments(orm.RelationOptions{}).Load(
		context.Background(), openScriptedDB(t, script), parents, orm.Select(relationCommentTable),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.queries) != 0 || len(loaded) != 2 || len(loaded[0].Comments) != 0 {
		t.Fatalf("empty-key load performed I/O or failed assignment: queries=%d loaded=%#v", len(script.queries), loaded)
	}
}

func TestRelationshipDefinitionsValidateGeneratedMetadata(t *testing.T) {
	if _, err := orm.NewBelongsTo(
		relationArticleTable, relationAuthorTable, relationArticleAuthorID, relationAuthorID,
		func(article relationArticle) (int64, bool) { return article.AuthorID, true },
		func(author relationAuthor) (int64, bool) { return author.ID, true },
		(func(*relationArticle, *relationAuthor))(nil), orm.RelationOptions{},
	); err == nil {
		t.Fatal("nil belongs-to assign callback succeeded")
	}
	if _, err := orm.NewManyToMany(
		relationArticleTable, relationTagTable, relationArticleID, relationTagID,
		"relation_article_tags;drop", "article_id", "tag_id",
		func(article relationArticle) (int64, bool) { return article.ID, true },
		func(tag relationTag) (int64, bool) { return tag.ID, true },
		func(orm.RowScanner) (int64, relationTag, error) { return 0, relationTag{}, nil },
		func(*relationArticle, []relationTag) {}, orm.RelationOptions{},
	); err == nil {
		t.Fatal("unsafe join identifier succeeded")
	}
	if _, err := orm.NewHasMany(
		relationArticleTable, relationCommentTable, relationArticleID, relationCommentArticleID,
		func(article relationArticle) (int64, bool) { return article.ID, true },
		func(comment relationComment) (int64, bool) { return comment.ArticleID, true },
		func(*relationArticle, []relationComment) {}, orm.RelationOptions{ParameterLimit: orm.PostgreSQLParameterLimit + 1},
	); err == nil {
		t.Fatal("invalid relation parameter limit succeeded")
	}
}

func TestRelationsRejectSameNamedDescriptorWithDifferentProvenance(t *testing.T) {
	duplicateAuthorTable := orm.MustTable("relation_authors", relationAuthorMapper)
	duplicateAuthorID := orm.MustColumn[relationAuthor, int64](duplicateAuthorTable, "id")
	if _, err := orm.Select(relationAuthorTable).Where(duplicateAuthorID.Eq(1)).Build(); err == nil {
		t.Fatal("predicate from same-named descriptor succeeded")
	}
	if _, err := belongsToAuthor(orm.RelationOptions{}).Build(
		[]relationArticle{{ID: 1, AuthorID: 1}}, orm.Select(duplicateAuthorTable),
	); err == nil {
		t.Fatal("scope from same-named descriptor succeeded")
	}
}

func TestZeroValueRelationsFailClosed(t *testing.T) {
	var belongsTo orm.BelongsTo[relationArticle, relationAuthor, int64]
	if _, err := belongsTo.Build(nil, orm.Select(relationAuthorTable)); err == nil {
		t.Fatal("zero-value belongs-to relation built a statement")
	}

	var hasOne orm.HasOne[relationAuthor, relationProfile, int64]
	if _, err := hasOne.Build(nil, orm.Select(relationProfileTable)); err == nil {
		t.Fatal("zero-value has-one relation built a statement")
	}

	var hasMany orm.HasMany[relationArticle, relationComment, int64]
	if _, err := hasMany.Build(nil, orm.Select(relationCommentTable)); err == nil {
		t.Fatal("zero-value has-many relation built a statement")
	}

	var manyToMany orm.ManyToMany[relationArticle, relationTag, int64, int64]
	if _, err := manyToMany.Build(nil, orm.Select(relationTagTable)); err == nil {
		t.Fatal("zero-value many-to-many relation built a statement")
	}
	if _, err := manyToMany.BuildAttach(1, 2); err == nil {
		t.Fatal("zero-value many-to-many relation built an attach")
	}
	if _, err := manyToMany.BuildDetach(1, 2); err == nil {
		t.Fatal("zero-value many-to-many relation built a detach")
	}
}

func TestRelationshipExecutionRejectsTypedNilExecutor(t *testing.T) {
	var nilDB *sql.DB
	parents := []relationArticle{{ID: 1, AuthorID: 1}}
	if _, err := belongsToAuthor(orm.RelationOptions{}).Load(
		context.Background(), nilDB, parents, orm.Select(relationAuthorTable),
	); err == nil {
		t.Fatal("belongs-to accepted typed-nil executor")
	}
	relation := articleManyTags(orm.RelationOptions{})
	if _, err := relation.Attach(context.Background(), nilDB, 1, 2); err == nil {
		t.Fatal("attach accepted typed-nil executor")
	}
	if _, err := relation.Detach(context.Background(), nilDB, 1, 2); err == nil {
		t.Fatal("detach accepted typed-nil executor")
	}
}
