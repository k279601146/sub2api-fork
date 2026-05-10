package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IDERelease stores app and engine release manifests consumed by IDE clients.
type IDERelease struct {
	ent.Schema
}

func (IDERelease) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "ide_releases"},
	}
}

func (IDERelease) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IDERelease) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind").
			MaxLen(20).
			NotEmpty(),
		field.String("version").
			MaxLen(64).
			NotEmpty(),
		field.String("min_app_version").
			MaxLen(64).
			Default(""),
		field.JSON("binaries", map[string]map[string]any{}).
			Default(func() map[string]map[string]any { return map[string]map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("release_notes").
			Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("is_mandatory").
			Default(false),
		field.Bool("is_latest").
			Default(true),
		field.Time("published_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (IDERelease) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "version").Unique(),
		index.Fields("kind", "is_latest"),
		index.Fields("published_at"),
	}
}
