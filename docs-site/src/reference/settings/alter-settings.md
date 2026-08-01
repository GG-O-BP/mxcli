# ALTER SETTINGS

## Synopsis

    ALTER SETTINGS MODEL key = value

    ALTER SETTINGS CONFIGURATION 'name' key = value

    ALTER SETTINGS CONSTANT 'name' VALUE 'value' IN CONFIGURATION 'config'

    ALTER SETTINGS DROP CONSTANT 'name' IN CONFIGURATION 'config'

    CREATE CONFIGURATION 'name' [key = value, ...]

    DROP CONFIGURATION 'name'

    ALTER SETTINGS LANGUAGE key = value

    ALTER SETTINGS WORKFLOWS key = value

## Description

Modifies project settings by category. Each category has its own syntax and available keys.

**MODEL** settings control application-level behavior such as the after-startup microflow, hashing algorithm, and Java version.

**CONFIGURATION** settings control named runtime configurations. Each project can have multiple configurations (e.g., `default`, `staging`, `production`). Settings include database type, database URL, HTTP port number, and other runtime parameters. The configuration name must be enclosed in single quotes.

**CONSTANT** settings override the default value of a project constant within a specific runtime configuration. Both the constant name and the configuration name must be enclosed in single quotes.

**LANGUAGE** settings control localization, primarily the default language code.

**WORKFLOWS** settings control the workflow engine, including the user entity used for workflow tasks and default task parallelism.

## Parameters

**key**
: The setting name to modify. Available keys depend on the category.

**value**
: The new value for the setting. String values must be enclosed in single quotes.

**name** (CONFIGURATION, CONSTANT)
: The name of the configuration or constant, enclosed in single quotes.

**config** (CONSTANT only)
: The name of the runtime configuration where the constant override applies, enclosed in single quotes.

## Examples

### Set the after-startup microflow

```sql
ALTER SETTINGS MODEL AfterStartupMicroflow = 'MyModule.ACT_Startup';
```

> **The after-startup microflow must return `Boolean`.** Mendix build fails with **CE0142**
> if it has no return type. A common trip-up is wiring a void seed/demo-data microflow to
> after-startup — end it with `return true` so it returns a Boolean.

### Configure database type

```sql
ALTER SETTINGS CONFIGURATION 'default' DatabaseType = 'POSTGRESQL';
```

### Set database URL for a configuration

```sql
ALTER SETTINGS CONFIGURATION 'production' DatabaseUrl = 'jdbc:postgresql://dbhost:5432/myapp';
```

### Override a constant in a configuration

```sql
ALTER SETTINGS CONSTANT 'MyModule.ApiBaseUrl' VALUE 'https://api.staging.example.com' IN CONFIGURATION 'staging';
```

An override's value is either **shared** — stored in the model, and so in version
control — or **private**, stored on the developer's own workstation and kept out of
the repository (the usual choice for development API tokens).

MDL preserves that choice but never changes it. `ALTER SETTINGS CONSTANT ... VALUE`
applies to shared values only; on a private override it is **refused**, because
setting a value would convert it to a shared one, publish a deliberately-local value
into version control, and break the developer's local binding. Change the constant to
a shared value in Studio Pro first, or drop the override. `DESCRIBE SETTINGS` reports
a private override as a comment rather than a re-executable statement, for the same
reason.

### Set the default language

```sql
ALTER SETTINGS LANGUAGE DefaultLanguageCode = 'en_US';
```

### Configure workflow user entity

```sql
ALTER SETTINGS WORKFLOWS UserEntity = 'Administration.Account';
```

### Set Java version

```sql
ALTER SETTINGS MODEL JavaVersion = '17';
```

### Remove a constant override from a configuration

```sql
ALTER SETTINGS DROP CONSTANT 'MyModule.ApiBaseUrl' IN CONFIGURATION 'staging';
```

### Create a new configuration

```sql
CREATE CONFIGURATION 'Staging'
  DatabaseType = 'POSTGRESQL',
  DatabaseUrl = 'staging-db:5432',
  HttpPortNumber = 8080;
```

### Drop a configuration

```sql
DROP CONFIGURATION 'Staging';
```

## See Also

[SHOW / DESCRIBE SETTINGS](show-settings.md)
