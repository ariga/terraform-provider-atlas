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
					DirURL: "../provider/migrations",
					URL:    fmt.Sprintf("%s/%s", mysqlURL, schema),
				},
			},
			wantTarget: "20221101165415",
		},
	}
	wd, err := os.Getwd()
	r.NoError(err)
	c, err := atlas.NewClient(context.Background(), wd, "atlas")
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
					DirURL: "../provider/migrations",
					URL:    fmt.Sprintf("%s/%s", mysqlURL, schema),
				},
			},
			wantCurrent: "No migration applied yet",
			wantNext:    "20221101163823",
		},
	}
	wd, err := os.Getwd()
	r.NoError(err)
	c, err := atlas.NewClient(context.Background(), wd, "atlas")
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
