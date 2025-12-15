package entimport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ariga.io/atlas/sql/postgres"
	"ariga.io/atlas/sql/schema"

	"entgo.io/contrib/schemast"
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// Postgres implements SchemaImporter for PostgreSQL databases.
type Postgres struct {
	*ImportOptions
}

// NewPostgreSQL - returns a new *Postgres.
func NewPostgreSQL(i *ImportOptions) (SchemaImporter, error) {
	return &Postgres{
		ImportOptions: i,
	}, nil
}

// SchemaMutations implements SchemaImporter.
func (p *Postgres) SchemaMutations(ctx context.Context) ([]schemast.Mutator, error) {
	inspectOptions := &schema.InspectOptions{
		Tables: p.tables,
	}
	s, err := p.driver.InspectSchema(ctx, p.driver.SchemaName, inspectOptions)
	if err != nil {
		return nil, err
	}
	tables := s.Tables
	if p.excludedTables != nil {
		tables = nil
		excludedTableNames := make(map[string]bool)
		for _, t := range p.excludedTables {
			excludedTableNames[t] = true
		}
		// filter out tables that are in excludedTables:
		for _, t := range s.Tables {
			if !excludedTableNames[t.Name] {
				tables = append(tables, t)
			}
		}
	}
	return schemaMutations(p.field, tables)
}

func (p *Postgres) field(column *schema.Column) (f ent.Field, err error) {
	name := column.Name
	switch typ := column.Type.Type.(type) {
	case *postgres.ArrayType:
		f, err = p.convertArray(typ, name)
	case *schema.BinaryType:
		f = field.Bytes(name)
	case *schema.BoolType:
		f = field.Bool(name)
	case *schema.DecimalType:
		f = field.Float(name)
	case *schema.EnumType:
		f = field.Enum(name).Values(typ.Values...)
	case *schema.FloatType:
		f = p.convertFloat(typ, name)
	case *schema.IntegerType:
		f = p.convertInteger(typ, name)
	case *schema.JSONType:
		f = field.JSON(name, json.RawMessage{})
	case *schema.StringType:
		f = field.String(name)
	case *schema.TimeType:
		f = field.Time(name)
	case *postgres.SerialType:
		f = p.convertSerial(typ, name)
	case *postgres.UUIDType:
		f = field.UUID(name, uuid.New())
	default:
		// Handle array types that Atlas wraps in sql()
		if strings.HasSuffix(column.Type.Raw, "[]") {
			f = p.convertRawArrayType(column.Type.Raw, name)
		} else {
			return nil, fmt.Errorf("entimport: unsupported type %q for column %v", typ, column.Name)
		}
	}
	applyColumnAttributes(f, column)
	return f, err
}

// decimal, numeric - user-specified precision, exact up to 131072 digits before the decimal point;
// up to 16383 digits after the decimal point.
// real - 4 bytes variable-precision, inexact 6 decimal digits precision.
// double -	8 bytes	variable-precision, inexact	15 decimal digits precision.
func (p *Postgres) convertFloat(typ *schema.FloatType, name string) (f ent.Field) {
	if typ.T == postgres.TypeReal {
		return field.Float32(name)
	}
	return field.Float(name)
}

func (p *Postgres) convertInteger(typ *schema.IntegerType, name string) (f ent.Field) {
	switch typ.T {
	// smallint - 2 bytes small-range integer -32768 to +32767.
	case "smallint":
		f = field.Int16(name)
	// integer - 4 bytes typical choice for integer	-2147483648 to +2147483647.
	case "integer":
		f = field.Int32(name)
	// bigint - 8 bytes large-range integer	-9223372036854775808 to 9223372036854775807.
	case "bigint":
		// Int64 is not used on purpose.
		f = field.Int(name)
	}
	return f
}

// smallserial- 2 bytes - small autoincrementing integer 1 to 32767
// serial - 4 bytes autoincrementing integer 1 to 2147483647
// bigserial - 8 bytes large autoincrementing integer	1 to 9223372036854775807
func (p *Postgres) convertSerial(typ *postgres.SerialType, name string) ent.Field {
	return field.Uint(name).
		SchemaType(map[string]string{
			dialect.Postgres: typ.T, // Override Postgres.
		})
}

// convertArray handles PostgreSQL array types.
func (p *Postgres) convertArray(typ *postgres.ArrayType, name string) (f ent.Field, err error) {
	// For text[] and varchar[] arrays, use Strings field
	switch typ.Type.(type) {
	case *schema.StringType:
		f = field.Strings(name)
	case *schema.IntegerType:
		f = field.JSON(name, []int{})
	case *schema.FloatType:
		f = field.JSON(name, []float64{})
	default:
		return nil, fmt.Errorf("entimport: unsupported array %+v %T for column %v", typ, typ, name)
	}
	return f, nil
}

// convertRawArrayType handles array types from raw SQL strings (e.g., "text[]", "integer[]").
func (p *Postgres) convertRawArrayType(rawType, name string) ent.Field {
	// Remove the [] suffix to get the base type
	baseType := strings.TrimSuffix(rawType, "[]")

	switch baseType {
	case "text", "varchar", "character varying":
		return field.Strings(name)
	case "smallint":
		return field.JSON(name, []int16{})
	case "integer", "int", "int4":
		return field.JSON(name, []int32{})
	case "bigint", "int8":
		return field.JSON(name, []int{})
	case "boolean", "bool":
		return field.JSON(name, []bool{})
	case "real", "float4":
		return field.JSON(name, []float32{})
	case "double precision", "float8":
		return field.JSON(name, []float64{})
	default:
		// For unknown array types, use generic JSON field
		return field.JSON(name, []interface{}{})
	}
}
