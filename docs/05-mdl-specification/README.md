# MDL Language Specification

MDL (Mendix Definition Language) is a SQL-like domain-specific language for defining and manipulating Mendix application models. This specification describes the language syntax and its mapping to various backends.

## Documents

1. [Language Reference](./01-language-reference.md) - Complete MDL syntax and semantics
2. [Data Types](./02-data-types.md) - MDL data type system
3. [Domain Model](./03-domain-model.md) - Entities, attributes, associations
4. [BSON Mapping](./10-bson-mapping.md) - Mapping to MPR file format
5. [Model SDK Mapping](./11-model-sdk-mapping.md) - Mapping to modelsdk-go library

For the comprehensive quick reference of all MDL statements, see [MDL Quick Reference](../MDL_QUICK_REFERENCE.md).

## Quick Reference

```sql
-- Connection
connect local '/path/to/app.mpr';
disconnect;
status;

-- Query
show modules;
show entities [in ModuleName];
show structure [depth 1|2|3] [in module] [all];
describe entity Module.EntityName;

-- Domain Model
create persistent entity Module.Name (
  AttrName: type [not null] [unique] [default value]
);
alter entity Module.Name add attribute NewAttr: string(200);
drop entity Module.Name;

-- Microflows
create microflow Module.Name begin ... end;
describe microflow Module.Name;

-- Pages
create page Module.Name (title: 'Title', layout: Module.Layout) { ... };
alter page Module.Name { set caption = 'New' on btnSave; };

-- Security
grant Module.Role on Module.Entity (create, delete, read *, write *);
grant execute on microflow Module.Name to Module.Role;

-- External SQL
sql connect postgres 'dsn' as alias;
sql alias select * from table;

-- Navigation, Settings, Business Events, Java Actions
-- See MDL Quick Reference for full syntax
```

## Design Principles

1. **SQL-like syntax** - Familiar to developers with database experience
2. **Case-insensitive keywords** - `create`, `create`, `create` are equivalent
3. **Qualified names** - `Module.Element` format for cross-module references
4. **Statement terminators** - `;` or `/` to end statements
5. **Multi-line support** - Statements can span multiple lines
6. **Documentation comments** - `/** ... */` for element documentation
