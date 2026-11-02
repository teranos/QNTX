// methods META OUT: writes acp_methods.h, every method meta.json names, as the SDK generates its tables from
// the same file: agentMethods, clientMethods and protocolMethods, each name to its wire name.
#include <json-c/json.h>

#include <ctype.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>

static const struct {
    const char *key;
    const char *prefix;
    const char *count;
} groups[] = {
    {"agentMethods", "ACP_AGENT_", "ACP_AGENT_METHODS"},
    {"clientMethods", "ACP_CLIENT_", "ACP_CLIENT_METHODS"},
    {"protocolMethods", "ACP_PROTOCOL_", "ACP_PROTOCOL_METHODS"},
};

static int fail(const char *meta, const char *what, const char *name) {
    fprintf(stderr, "methods: %s: %s%s%s\n", meta, what, name != NULL ? ": " : "", name != NULL ? name : "");
    return 1;
}

int main(int argc, char **argv) {
    if (argc != 3) {
        fprintf(stderr, "usage: methods META OUT\n");
        return 2;
    }
    const char *meta = argv[1];
    json_object *root = json_object_from_file(meta);
    if (!json_object_is_type(root, json_type_object)) return fail(meta, "is no JSON object", json_util_get_last_err());
    FILE *out = fopen(argv[2], "w");
    if (out == NULL) return fail(argv[2], "could not be written", strerror(errno));
    fprintf(out, "// Generated from %s by tools/methods.c: every ACP method by its name there.\n", meta);
    fprintf(out, "#ifndef ACP_METHODS_H\n#define ACP_METHODS_H\n");
    for (size_t g = 0; g < sizeof groups / sizeof *groups; g++) {
        json_object *methods = NULL;
        if (!json_object_object_get_ex(root, groups[g].key, &methods) || !json_object_is_type(methods, json_type_object)) {
            fclose(out);
            return fail(meta, "names no object", groups[g].key);
        }
        fprintf(out, "\n");
        size_t count = 0;
        json_object_object_foreach(methods, name, wire) {
            if (!json_object_is_type(wire, json_type_string)) {
                fclose(out);
                return fail(meta, "a method's wire name is no string", name);
            }
            // A name is a C identifier once upper-cased, and a wire name needs no escape in a C string.
            for (const char *c = name; *c != '\0'; c++) {
                if (!islower((unsigned char) *c) && !isdigit((unsigned char) *c) && *c != '_') {
                    fclose(out);
                    return fail(meta, "a method's name is no identifier", name);
                }
            }
            for (const char *c = json_object_get_string(wire); *c != '\0'; c++) {
                if (*c == '"' || *c == '\\' || !isprint((unsigned char) *c)) {
                    fclose(out);
                    return fail(meta, "a wire name holds what a C string must escape", name);
                }
            }
            fprintf(out, "#define %s", groups[g].prefix);
            for (const char *c = name; *c != '\0'; c++) fputc(toupper((unsigned char) *c), out);
            fprintf(out, " \"%s\"\n", json_object_get_string(wire));
            count++;
        }
        fprintf(out, "#define %s %zu\n", groups[g].count, count);
    }
    fprintf(out, "\n#endif\n");
    json_object_put(root);
    if (fclose(out) != 0) return fail(argv[2], "could not be written", strerror(errno));
    return 0;
}
