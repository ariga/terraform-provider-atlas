package atlas_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "ariga.io/atlas/sql/mysql"
	_ "github.com/go-sql-driver/mysql"

	"ariga.io/ariga/terraform-provider-atlas/internal/atlas"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/stretchr/testify/require"
)

const (
	mysqlURL = "mysql://root:pass@localhost:3306"
)

func Test_MigrateApply(t *testing.T) {
	schema := "test"
	tempSchemas(t, schema)
	r := require.New(t)
	type args struct {
		ctx  context.Context
		data *atlas.ApplyParams
	}
	tests := []struct {
		name       string
		args       args
		wantTarget string
		wantErr    bool
	}{
		{
			args: args{
				ctx: context.Background(),
				data: &atlas.ApplyParams{
					DirURL: "file://../provider/migrations",
					URL:    fmt.Sprintf("%s/%s", mysqlURL, schema),
					Format: "{{ json . }}",
				},
			},
			wantTarget: "20221101165415",
		},
	}
	wd, err := os.Getwd()
	r.NoError(err)
	c, err := atlas.NewClient(wd, "atlas")
	r.NoError(err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Apply(tt.args.ctx, tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("migrateApply() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.EqualValues(t, tt.wantTarget, got.Target)
		})
	}
}

func Test_MigrateStatus(t *testing.T) {
	schema := "test"
	tempSchemas(t, schema)
	r := require.New(t)
	type args struct {
		ctx  context.Context
		data *atlas.StatusParams
	}
	tests := []struct {
		name        string
		args        args
		wantCurrent string
		wantNext    string
		wantErr     bool
	}{
		{
			args: args{
				ctx: context.Background(),
				data: &atlas.StatusParams{
					DirURL: "file://../provider/migrations",
					URL:    fmt.Sprintf("%s/%s", mysqlURL, schema),
					Format: "{{ json . }}",
				},
			},
			wantCurrent: "No migration applied yet",
			wantNext:    "20221101163823",
		},
	}
	wd, err := os.Getwd()
	r.NoError(err)
	c, err := atlas.NewClient(wd, "atlas")
	r.NoError(err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Status(tt.args.ctx, tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("migrateStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.Equal(t, tt.wantCurrent, got.Current)
			require.Equal(t, tt.wantNext, got.Next)
		})
	}
}

func Test_SchemaApply(t *testing.T) {
	f, err := os.CreateTemp("", "sqlite-test")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	u := fmt.Sprintf("sqlite://%s?_fk=1", f.Name())
	wd, err := os.Getwd()
	require.NoError(t, err)
	c, err := atlas.NewClient(wd, "atlas")
	require.NoError(t, err)

	s1 := `
	-- create table "users
	CREATE TABLE users(
		id int NOT NULL,
		name varchar(100) NULL,
		PRIMARY KEY(id)
	);
	`
	to, clean, err := atlas.TempFile(s1, "sql")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, clean())
	}()
	_, err = c.SchemaApply(context.Background(), &atlas.SchemaApplyParams{
		URL:    u,
		To:     to,
		DevURL: "sqlite://file?_fk=1&cache=shared&mode=memory",
	})
	require.NoError(t, err)

	s2 := s1 + `
	-- create table "blog_posts"
	CREATE TABLE blog_posts(
		id int NOT NULL,
		title varchar(100) NULL,
		body text NULL,
		author_id int NULL,
		PRIMARY KEY(id),
		CONSTRAINT author_fk FOREIGN KEY(author_id) REFERENCES users(id)
	);`
	to, clean2, err := atlas.TempFile(s2, "sql")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, clean2())
	}()
	_, err = c.SchemaApply(context.Background(), &atlas.SchemaApplyParams{
		URL:    u,
		To:     to,
		DevURL: "sqlite://file?_fk=1&cache=shared&mode=memory",
	})
	require.NoError(t, err)

	s, err := c.SchemaInspect(context.Background(), &atlas.SchemaInspectParams{
		URL: u,
	})
	require.NoError(t, err)
	require.Equal(t, `table "users" {
  schema = schema.main
  column "id" {
    null = false
    type = int
  }
  column "name" {
    null = true
    type = varchar
  }
  primary_key {
    columns = [column.id]
  }
}
table "blog_posts" {
  schema = schema.main
  column "id" {
    null = false
    type = int
  }
  column "title" {
    null = true
    type = varchar
  }
  column "body" {
    null = true
    type = text
  }
  column "author_id" {
    null = true
    type = int
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "author_fk" {
    columns     = [column.author_id]
    ref_columns = [table.users.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
}
schema "main" {
}
`, s)
}

func tempSchemas(t *testing.T, schemas ...string) {
	c, err := sqlclient.Open(context.Background(), mysqlURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		_, err := c.ExecContext(context.Background(), fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", s))
		if err != nil {
			t.Errorf("failed creating schema: %s", err)
		}
	}
	drop(t, c, schemas...)
}

func drop(t *testing.T, c *sqlclient.Client, schemas ...string) {
	t.Cleanup(func() {
		t.Log("Dropping all schemas")
		for _, s := range schemas {
			_, err := c.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", s))
			if err != nil {
				t.Errorf("failed dropping schema: %s", err)
			}
		}
		if err := c.Close(); err != nil {
			t.Errorf("failed closing client: %s", err)
		}
	})
}
