# Starlark Lint Rule: Missing Documentation
#
# Undocumented model elements are invisible to `mxcli check` and to the build —
# nothing fails, so nothing reminds you. This rule is the reminder.
#
# Checks, each independently switchable (see Options):
#   - Entities              .description
#   - Microflows            .description   (nanoflows and trivial flows exempt)
#   - Java actions          .documentation
#   - Java action params    .description   <- the one Studio Pro shows a CALLER
#   - Entity attributes     .description   (off by default: high volume)
#
# Why parameters matter more than they look: Studio Pro renders a Java action's
# parameter descriptions in the dialog where someone wires up the call. An
# undocumented parameter is a blank field next to a name like `pInput`, at
# exactly the moment a caller has to decide what to pass.
#
# Options (lint config `options:` block for QUAL002):
#   check_entities             bool, default True
#   check_microflows           bool, default True
#   check_java_actions         bool, default True
#   check_java_action_params   bool, default True
#   check_attributes           bool, default False
#   min_activities             int,  default 3   (microflows below this are exempt)

RULE_ID = "QUAL002"
RULE_NAME = "Missing Documentation"
DESCRIPTION = "Model elements should have documentation describing their purpose"
CATEGORY = "quality"
SEVERITY = "info"

def _blank(text):
    """True when a documentation field is absent or whitespace-only."""
    return not text or text.strip() == ""

def _flag(violations, module, doc_type, doc_name, message, suggestion):
    violations.append(violation(
        message = message,
        location = location(
            module = module,
            document_type = doc_type,
            document_name = doc_name,
        ),
        suggestion = suggestion,
    ))

def check():
    violations = []

    if get_option("check_entities", True):
        for entity in entities():
            if _blank(entity.description):
                _flag(
                    violations,
                    entity.module_name,
                    "Entity",
                    entity.qualified_name,
                    "Entity '{}' has no documentation.".format(entity.name),
                    "Add a description explaining the entity's purpose and what data it represents.",
                )

    if get_option("check_attributes", False):
        for entity in entities():
            for attr in attributes_for(entity.qualified_name):
                if _blank(attr.description):
                    _flag(
                        violations,
                        entity.module_name,
                        "Entity",
                        entity.qualified_name,
                        "Attribute '{}.{}' has no documentation.".format(entity.name, attr.name),
                        "Add a description, or switch this off with `check_attributes: false` if " +
                        "attribute names are self-describing in this project.",
                    )

    if get_option("check_microflows", True):
        # Nanoflows are excluded: they are usually a couple of client-side steps
        # whose name says everything a description would.
        min_activities = get_option("min_activities", 3)
        for mf in microflows():
            if mf.microflow_type != "MICROFLOW":
                continue
            if mf.activity_count < min_activities:
                continue
            if _blank(mf.description):
                _flag(
                    violations,
                    mf.module_name,
                    "Microflow",
                    mf.qualified_name,
                    "Microflow '{}' has no documentation.".format(mf.name),
                    "Add a description explaining what this microflow does and when it should be called.",
                )

    check_actions = get_option("check_java_actions", True)
    check_params = get_option("check_java_action_params", True)
    if check_actions or check_params:
        for ja in java_actions():
            if check_actions and _blank(ja.documentation):
                _flag(
                    violations,
                    ja.module_name,
                    "JavaAction",
                    ja.qualified_name,
                    "Java action '{}' has no documentation.".format(ja.name),
                    "Add documentation explaining what the action does, and what it returns. " +
                    "Unlike a microflow, its body is Java that the model cannot show a reader.",
                )
            if not check_params:
                continue
            for p in ja.parameters:
                if _blank(p.description):
                    _flag(
                        violations,
                        ja.module_name,
                        "JavaAction",
                        ja.qualified_name,
                        "Java action parameter '{}.{}' has no description.".format(ja.name, p.name),
                        "Add a description: Studio Pro shows it to whoever wires up the call, " +
                        "where the parameter name is all they otherwise have to go on.",
                    )

    return violations
