package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IDESession stores desktop IDE authentication sessions.
type IDESession struct {
	ent.Schema
}

func (IDESession) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "ide_sessions"},
	}
}

func (IDESession) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IDESession) Fields() []ent.Field {
	return []ent.Field{
		field.String("session_id").
			MaxLen(128).
			Unique().
			Immutable().
			NotEmpty(),
		field.Int64("user_id"),
		field.String("jwt_token_hash").
			MaxLen(128).
			Unique().
			NotEmpty(),
		field.String("client_id").
			MaxLen(128).
			Default("myide-desktop"),
		field.String("client_version").
			MaxLen(64).
			Default(""),
		field.String("platform").
			MaxLen(64).
			Default(""),
		field.String("device_id").
			MaxLen(255).
			Default(""),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_used_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Bool("revoked").
			Default(false),
		field.String("revoke_reason").
			MaxLen(64).
			Default(""),
	}
}

func (IDESession) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("ide_sessions").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (IDESession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("jwt_token_hash").Unique(),
		index.Fields("user_id", "revoked"),
		index.Fields("expires_at"),
		index.Fields("last_used_at"),
	}
}
