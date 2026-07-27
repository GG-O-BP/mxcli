# Use Cases

This page provides practical catalog query examples for common analysis tasks: impact analysis, finding unused elements, detecting anti-patterns, and computing complexity metrics.

## Duplicate Entity Detection

Find entities with similar names across modules that may be candidates for consolidation:

```sql
SELECT
    e1.ModuleName AS Module1,
    e1.Name AS Entity1,
    e2.ModuleName AS Module2,
    e2.Name AS Entity2
FROM CATALOG.ENTITIES e1
JOIN CATALOG.ENTITIES e2
    ON LOWER(e1.Name) = LOWER(e2.Name)
    AND e1.ModuleName < e2.ModuleName
ORDER BY e1.Name;
```

Detect copy-paste inheritance anti-patterns by finding entities with identical attribute patterns:

```sql
SELECT
    e1.QualifiedName AS Entity1,
    e2.QualifiedName AS Entity2,
    COUNT(*) AS MatchingAttributes
FROM CATALOG.ATTRIBUTES a1
JOIN CATALOG.ATTRIBUTES a2
    ON a1.Name = a2.Name
    AND a1.AttributeType = a2.AttributeType
    AND a1.EntityId != a2.EntityId
JOIN CATALOG.ENTITIES e1 ON a1.EntityId = e1.Id
JOIN CATALOG.ENTITIES e2 ON a2.EntityId = e2.Id
WHERE e1.QualifiedName < e2.QualifiedName
GROUP BY e1.QualifiedName, e2.QualifiedName
HAVING COUNT(*) >= 3
ORDER BY MatchingAttributes DESC;
```

## Data Lineage Analysis

### Entity Dependency Graph

Find all entities connected to a given entity via associations:

```sql
SELECT
    'Outgoing' AS Direction,
    a.Name AS Association,
    a.ChildEntity AS RelatedEntity,
    a.AssociationType AS Type
FROM CATALOG.ASSOCIATIONS a
WHERE a.ParentEntity = 'Sales.Order'
UNION ALL
SELECT
    'Incoming' AS Direction,
    a.Name AS Association,
    a.ParentEntity AS RelatedEntity,
    a.AssociationType AS Type
FROM CATALOG.ASSOCIATIONS a
WHERE a.ChildEntity = 'Sales.Order';
```

### Microflows That Modify an Entity

Track all business logic touching specific data:

```sql
SELECT
    m.QualifiedName AS Microflow,
    m.ReturnType,
    m.Description
FROM CATALOG.MICROFLOWS m
WHERE m.ObjectUsage LIKE '%Sales.Customer%'
   OR m.Parameters LIKE '%Sales.Customer%'
   OR m.ReturnType LIKE '%Sales.Customer%';
```

### Page-to-Entity Mapping

Which pages display or edit which entities:

```sql
SELECT
    p.QualifiedName AS Page,
    p.DataSource AS Entity,
    p.WidgetTypes
FROM CATALOG.PAGES p
WHERE p.DataSource IS NOT NULL
ORDER BY p.DataSource, p.QualifiedName;
```

## Orphan Detection

Find persistent entities with no associations, which may indicate missing relationships:

```sql
SELECT e.QualifiedName, e.EntityType, e.AttributeCount
FROM CATALOG.ENTITIES e
LEFT JOIN CATALOG.ASSOCIATIONS a
    ON e.QualifiedName = a.ParentEntity
    OR e.QualifiedName = a.ChildEntity
WHERE a.Id IS NULL
  AND e.EntityType = 'Persistent'
ORDER BY e.ModuleName, e.Name;
```

## Complexity Analysis

Find entities with high complexity (many attributes and associations):

```sql
SELECT
    e.QualifiedName,
    e.AttributeCount,
    COUNT(a.Id) AS AssociationCount,
    e.AttributeCount + COUNT(a.Id) AS ComplexityScore
FROM CATALOG.ENTITIES e
LEFT JOIN CATALOG.ASSOCIATIONS a
    ON e.QualifiedName = a.ParentEntity
    OR e.QualifiedName = a.ChildEntity
GROUP BY e.QualifiedName, e.AttributeCount
HAVING ComplexityScore > 15
ORDER BY ComplexityScore DESC;
```

## Documentation Coverage

Identify technical debt in documentation:

```sql
-- Entities without documentation
SELECT QualifiedName, EntityType, AttributeCount
FROM CATALOG.ENTITIES
WHERE (Description IS NULL OR Description = '')
ORDER BY AttributeCount DESC;

-- Documentation coverage by module
SELECT
    ModuleName,
    COUNT(*) AS TotalEntities,
    SUM(CASE WHEN Description != '' THEN 1 ELSE 0 END) AS Documented,
    ROUND(100.0 * SUM(CASE WHEN Description != '' THEN 1 ELSE 0 END) / COUNT(*), 1) AS CoveragePercent
FROM CATALOG.ENTITIES
GROUP BY ModuleName
ORDER BY CoveragePercent ASC;
```

## Security Analysis

### Entities Without Access Rules

```sql
SELECT
    e.QualifiedName,
    e.EntityType,
    e.AccessRuleCount
FROM CATALOG.ENTITIES e
WHERE e.AccessRuleCount = 0
  AND e.EntityType = 'Persistent'
ORDER BY e.ModuleName;
```

### Wide-Open Entities

Review entities accessible to anonymous users:

```sql
SELECT
    e.QualifiedName,
    e.AttributeCount,
    ar.UserRole,
    ar.AllowRead,
    ar.AllowWrite
FROM CATALOG.ENTITIES e
JOIN CATALOG.ACCESS_RULES ar ON e.Id = ar.EntityId
WHERE ar.AllowRead = 1
  AND ar.UserRole = 'Anonymous'
ORDER BY e.AttributeCount DESC;
```

## Module Health Dashboard

Overview of module complexity and documentation:

```sql
SELECT
    ModuleName,
    COUNT(DISTINCT e.Id) AS Entities,
    COUNT(DISTINCT m.Id) AS Microflows,
    COUNT(DISTINCT p.Id) AS Pages,
    ROUND(100.0 * SUM(CASE WHEN e.Description != '' THEN 1 ELSE 0 END) /
        NULLIF(COUNT(DISTINCT e.Id), 0), 1) AS DocCoverage
FROM CATALOG.ENTITIES e
LEFT JOIN CATALOG.MICROFLOWS m ON e.ModuleName = m.ModuleName
LEFT JOIN CATALOG.PAGES p ON e.ModuleName = p.ModuleName
GROUP BY ModuleName
ORDER BY Entities DESC;
```

## Naming Convention Violations

Find entities not following PascalCase:

```sql
SELECT QualifiedName, Name
FROM CATALOG.ENTITIES
WHERE Name != UPPER(SUBSTR(Name, 1, 1)) || SUBSTR(Name, 2)
   OR Name LIKE '%_%'
   OR Name LIKE '% %';
```

## Query the catalog directly (and join it to other sources)

The catalog **is** a SQLite database — `<projectDir>/.mxcli/catalog.db`, written
next to the `.mpr` and refreshed by `refresh catalog`. You do not need to export
it to JSON (that drags in human-readable formatting, exposes only a subset of the
tables, and goes stale immediately). Attach the file directly with any SQLite- or
DuckDB-compatible tool for ad-hoc analysis beyond what `SELECT … FROM CATALOG.*`
exposes in one query.

> **Run `refresh catalog full` first for the analytic tables.** Plain
> `refresh catalog` (fast mode) leaves `activities` / `refs` / `xpath_expressions`
> (and `widgets`) **empty** — a fast-mode query against them warns
> *"requires refresh catalog full"*. Full mode populates them (activities carry
> the model GUIDs the debugger uses, one row per activity with its sequence).

```bash
# from a dev container, after: mxcli -p app.mpr -c "refresh catalog full"
duckdb -c "ATTACH '.mxcli/catalog.db' AS cat (TYPE sqlite, READ_ONLY);
           SELECT MicroflowQualifiedName, COUNT(*) AS activities
           FROM cat.activities_data GROUP BY 1 ORDER BY 2 DESC LIMIT 10;"
```

### Cross-source "app warehouse" (dev container)

Because the catalog is a plain database, one engine can join it to the app's data
and its telemetry without any ETL — useful for questions no single source can
answer (e.g. runtime cost per microflow *joined to* model shape, or query time per
entity *joined to* live row counts). This is an **external** DuckDB recipe — mxcli
does not embed DuckDB — scoped to dev data in the dev container:

```sql
-- model metadata (catalog) + app data (the local app Postgres), both read-only
ATTACH '.mxcli/catalog.db' AS cat (TYPE sqlite, READ_ONLY);
ATTACH 'dbname=app host=127.0.0.1 user=mendix' AS app (TYPE postgres, READ_ONLY);
-- traces exported by `mxcli run --local --trace-otlp …` (or a JSONL span dump)
SELECT * FROM read_json_auto('spans.jsonl') LIMIT 10;
```

Caveats: keep every attachment **read-only** (especially the app database — never
attach a production DB, and even locally make it an explicit choice), and note that
unfiltered per-activity tracing produces a very large span volume (~110k spans for
one busy transaction), so filter or sample before loading traces.
