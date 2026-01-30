/**
 * QNTX AX Query Parser - C API
 *
 * High-performance AX query parser for QNTX.
 * This header provides the C interface for integration with Go via CGO.
 *
 * Memory Ownership Rules:
 * - AxQueryResultC contains owned pointers that must be freed
 * - Use parser_result_free() to deallocate results
 * - Use parser_string_free() for individual strings
 *
 * Thread Safety:
 * - The parser is stateless and thread-safe
 * - Multiple threads can parse concurrently
 */

#ifndef QNTX_AX_PARSER_H
#define QNTX_AX_PARSER_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * Temporal clause type.
 */
typedef enum {
    TEMPORAL_NONE = 0,     /* No temporal clause */
    TEMPORAL_SINCE = 1,    /* "since DATE" */
    TEMPORAL_UNTIL = 2,    /* "until DATE" */
    TEMPORAL_ON = 3,       /* "on DATE" */
    TEMPORAL_BETWEEN = 4,  /* "between DATE and DATE" */
    TEMPORAL_OVER = 5,     /* "over DURATION" */
} TemporalTypeC;

/**
 * Duration unit for "over" comparisons.
 */
typedef enum {
    DURATION_UNKNOWN = 0,
    DURATION_YEARS = 1,
    DURATION_MONTHS = 2,
    DURATION_WEEKS = 3,
    DURATION_DAYS = 4,
} DurationUnitC;

/**
 * Temporal clause data.
 */
typedef struct {
    TemporalTypeC temporal_type;  /* Type of temporal clause */
    char *start;                   /* Start date (for Since, Until, On, Between) */
    char *end;                     /* End date (for Between only) */
    double duration_value;         /* Duration value (for Over only) */
    DurationUnitC duration_unit;   /* Duration unit (for Over only) */
    char *duration_raw;            /* Raw duration string (for Over only) */
} TemporalClauseC;

/**
 * Result of parsing an AX query.
 * Must be freed with parser_result_free().
 */
typedef struct {
    bool success;           /* True if parsing succeeded */
    char *error_msg;        /* Error message if !success (owned) */
    size_t error_position;  /* Error position in input (byte offset) */

    char **subjects;        /* Subject strings (owned array of owned strings) */
    size_t subjects_len;    /* Number of subjects */

    char **predicates;      /* Predicate strings (owned array) */
    size_t predicates_len;  /* Number of predicates */

    char **contexts;        /* Context strings (owned array) */
    size_t contexts_len;    /* Number of contexts */

    char **actors;          /* Actor strings (owned array) */
    size_t actors_len;      /* Number of actors */

    TemporalClauseC temporal;  /* Temporal clause */

    char **actions;         /* Action strings (owned array) */
    size_t actions_len;     /* Number of actions */

    uint64_t parse_time_us; /* Parse time in microseconds */
} AxQueryResultC;

/* ============================================================================
 * Parsing
 * ============================================================================ */

/**
 * Parse an AX query string.
 *
 * @param query Null-terminated UTF-8 query string.
 * @return AxQueryResultC with parsed components.
 *         Must free with parser_result_free().
 */
AxQueryResultC parser_parse_query(const char *query);

/* ============================================================================
 * Memory Management
 * ============================================================================ */

/**
 * Free an AxQueryResultC and all contained strings.
 *
 * @param result Result to free.
 */
void parser_result_free(AxQueryResultC result);

/**
 * Free a string returned by parser functions.
 *
 * @param s String to free (safe to pass NULL).
 */
void parser_string_free(char *s);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_AX_PARSER_H */
