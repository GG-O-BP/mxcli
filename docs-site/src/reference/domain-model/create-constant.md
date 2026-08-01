# CREATE CONSTANT

## Synopsis

    CREATE [ OR MODIFY ] CONSTANT module.name TYPE data_type [ DEFAULT value ]

## Description

`CREATE CONSTANT` defines a named constant in a module. Constants hold configuration values (API URLs, feature flags, limits) that can differ between environments. In Mendix, constant values can be overridden per runtime configuration without changing the model.

The `TYPE` clause specifies the constant's data type. Supported types include `String`, `Integer`, `Long`, `Decimal`, `Boolean`, and `DateTime`.

The optional `DEFAULT` clause sets the constant's default value. This is the value used at runtime unless overridden by environment configuration.

If `OR MODIFY` is specified, the statement is idempotent. If the constant already exists, its type and default value are updated.

Constant values can also be overridden per deployment configuration using `ALTER SETTINGS CONSTANT`.

## Parameters

**OR MODIFY**
: Makes the statement idempotent. If the constant already exists, its definition is updated. Without this clause, creating a duplicate constant is an error.

**module.name**
: The qualified name of the constant in the form `Module.ConstantName`. The module must already exist.

**data_type**
: The constant's data type. One of: `String`, `Integer`, `Long`, `Decimal`, `Boolean`, `DateTime`.

**DEFAULT value**
: The default value for the constant. String values are single-quoted. Numeric values are bare. Boolean values are `true` or `false`.

## Examples

### String constant for API URL

```sql
CREATE CONSTANT MyModule.ApiBaseUrl TYPE String DEFAULT 'https://api.example.com';
```

### Integer constant for configuration

```sql
CREATE CONSTANT MyModule.MaxRetries TYPE Integer DEFAULT 3;
```

### Boolean feature flag

```sql
CREATE CONSTANT MyModule.EnableLogging TYPE Boolean DEFAULT true;
```

### Idempotent with OR MODIFY

```sql
CREATE OR MODIFY CONSTANT MyModule.ApiBaseUrl TYPE String DEFAULT 'https://api.example.com/v2';
```

### Constant with an empty default

A `DEFAULT` is always required. For secrets, use an empty default and set the
real value per configuration (see below):

```sql
CREATE CONSTANT MyModule.DatabasePassword TYPE String DEFAULT '';
```

### Override constant per configuration

```sql
-- Create the constant
CREATE CONSTANT MyModule.ApiBaseUrl TYPE String DEFAULT 'https://api.example.com';

-- Override in a specific runtime configuration
ALTER SETTINGS CONSTANT 'MyModule.ApiBaseUrl' VALUE 'https://staging.example.com' IN CONFIGURATION 'Staging';
```

### Shared and private values

An override holds its value one of two ways:

- **Shared** — stored in the model, so it travels with the project in version
  control and every developer gets it.
- **Private** — stored on the developer's own workstation and deliberately kept
  out of the repository. This is the answer for a development secret: the
  constant and the override are shared, the value is not.

MDL **preserves that choice but never changes it** — the shared/private decision
belongs to the constant, and configurations just respect it:

| Statement | On a private override |
|-----------|----------------------|
| `ALTER SETTINGS CONSTANT … VALUE …` | **refused** — setting a value would convert it to shared and publish a deliberately-local value into version control |
| `ALTER SETTINGS DROP CONSTANT …` | allowed — removes the whole override, which is what was asked for |
| `SHOW CONSTANT VALUES` | reports `(private)` rather than a blank cell |
| `DESCRIBE SETTINGS` | emits a comment, not a re-executable statement |

To make a private value shared (or the reverse), change it in Studio Pro.

## See Also

[CREATE ENTITY](create-entity.md), [CREATE ENUMERATION](create-enumeration.md)
