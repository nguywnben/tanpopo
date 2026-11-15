#ifndef TANPOPO_WASM_COMMON_H
#define TANPOPO_WASM_COMMON_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define WASM_EXPORT(name) __attribute__((visibility("default"), export_name(name)))

extern unsigned char __heap_base;
static uintptr_t tp_heap_ptr = 0;

static inline void *xmemset(void *dst, int c, size_t n) {
    unsigned char *d = (unsigned char *)dst;
    for (size_t i = 0; i < n; i++) {
        d[i] = (unsigned char)c;
    }
    return dst;
}

static inline void *xmemcpy(void *dst, const void *src, size_t n) {
    unsigned char *d = (unsigned char *)dst;
    const unsigned char *s = (const unsigned char *)src;
    for (size_t i = 0; i < n; i++) {
        d[i] = s[i];
    }
    return dst;
}

static inline size_t xstrlen(const char *s) {
    if (!s) return 0;
    size_t n = 0;
    while (s[n] != '\0') n++;
    return n;
}

static inline int xmemcmp(const void *a, const void *b, size_t n) {
    const unsigned char *pa = (const unsigned char *)a;
    const unsigned char *pb = (const unsigned char *)b;
    for (size_t i = 0; i < n; i++) {
        if (pa[i] != pb[i]) return (int)pa[i] - (int)pb[i];
    }
    return 0;
}

static inline char xlower(char c) {
    if (c >= 'A' && c <= 'Z') return (char)(c + ('a' - 'A'));
    return c;
}

static inline bool xstreq_n(const char *a, uint32_t alen, const char *b) {
    if (!a || !b) return false;
    uint32_t blen = (uint32_t)xstrlen(b);
    if (alen != blen) return false;
    return xmemcmp(a, b, alen) == 0;
}

static inline bool xstrieq_n(const char *a, uint32_t alen, const char *b) {
    if (!a || !b) return false;
    uint32_t blen = (uint32_t)xstrlen(b);
    if (alen != blen) return false;
    for (uint32_t i = 0; i < alen; i++) {
        if (xlower(a[i]) != xlower(b[i])) return false;
    }
    return true;
}

static inline bool xstrstarts(const char *s, uint32_t slen, const char *prefix) {
    uint32_t plen = (uint32_t)xstrlen(prefix);
    if (slen < plen) return false;
    return xmemcmp(s, prefix, plen) == 0;
}

static inline int32_t xstrfind(const char *hay, uint32_t hlen, const char *needle) {
    uint32_t nlen = (uint32_t)xstrlen(needle);
    if (nlen == 0) return 0;
    if (hlen < nlen) return -1;
    for (uint32_t i = 0; i <= hlen - nlen; i++) {
        if (xmemcmp(hay + i, needle, nlen) == 0) return (int32_t)i;
    }
    return -1;
}

static inline int32_t xstrifind(const char *hay, uint32_t hlen, const char *needle) {
    uint32_t nlen = (uint32_t)xstrlen(needle);
    if (nlen == 0) return 0;
    if (hlen < nlen) return -1;
    for (uint32_t i = 0; i <= hlen - nlen; i++) {
        bool ok = true;
        for (uint32_t j = 0; j < nlen; j++) {
            if (xlower(hay[i + j]) != xlower(needle[j])) {
                ok = false;
                break;
            }
        }
        if (ok) return (int32_t)i;
    }
    return -1;
}

static inline void *arena_alloc(size_t sz) {
    if (tp_heap_ptr == 0) {
        tp_heap_ptr = ((uintptr_t)&__heap_base + 7u) & ~(uintptr_t)7u;
    }
    sz = (sz + 7u) & ~(size_t)7u;
    if (sz == 0) sz = 8;
    size_t cur_bytes = (size_t)__builtin_wasm_memory_size(0) * 65536u;
    if (tp_heap_ptr + sz > cur_bytes) {
        size_t need_bytes = (tp_heap_ptr + sz) - cur_bytes;
        size_t need_pages = (need_bytes + 65535u) / 65536u;
        if (__builtin_wasm_memory_grow(0, (int)need_pages) < 0) {
            __builtin_trap();
        }
    }
    void *p = (void *)tp_heap_ptr;
    tp_heap_ptr += sz;
    xmemset(p, 0, sz);
    return p;
}

static inline char *arena_strndup(const char *s, uint32_t len) {
    char *p = (char *)arena_alloc((size_t)len + 1u);
    if (len > 0 && s) xmemcpy(p, s, len);
    p[len] = '\0';
    return p;
}

WASM_EXPORT("tanpopo_alloc")
uint32_t tanpopo_alloc(uint32_t sz) {
    return (uint32_t)(uintptr_t)arena_alloc((size_t)sz);
}

typedef struct {
    char *data;
    uint32_t len;
    uint32_t cap;
} strbuf_t;

static inline void sb_init(strbuf_t *sb, uint32_t init_cap) {
    if (init_cap < 256) init_cap = 256;
    sb->data = (char *)arena_alloc(init_cap);
    sb->len = 0;
    sb->cap = init_cap;
}

static inline void sb_reserve(strbuf_t *sb, uint32_t add) {
    if (sb->len + add + 1u <= sb->cap) return;
    uint32_t ncap = sb->cap * 2u;
    while (ncap < sb->len + add + 1u) ncap *= 2u;
    char *ndata = (char *)arena_alloc(ncap);
    if (sb->len > 0) xmemcpy(ndata, sb->data, sb->len);
    sb->data = ndata;
    sb->cap = ncap;
}

static inline void sb_putc(strbuf_t *sb, char c) {
    sb_reserve(sb, 1u);
    sb->data[sb->len++] = c;
    sb->data[sb->len] = '\0';
}

static inline void sb_putn(strbuf_t *sb, const char *s, uint32_t n) {
    if (!s || n == 0) return;
    sb_reserve(sb, n);
    xmemcpy(sb->data + sb->len, s, n);
    sb->len += n;
    sb->data[sb->len] = '\0';
}

static inline void sb_puts(strbuf_t *sb, const char *s) {
    if (!s) return;
    sb_putn(sb, s, (uint32_t)xstrlen(s));
}

static inline void sb_put_i64(strbuf_t *sb, int64_t v) {
    char tmp[32];
    int pos = 0;
    uint64_t uv;
    if (v < 0) {
        sb_putc(sb, '-');
        uv = (uint64_t)(-(v + 1)) + 1ULL;
    } else {
        uv = (uint64_t)v;
    }
    if (uv == 0) {
        sb_putc(sb, '0');
        return;
    }
    while (uv > 0) {
        tmp[pos++] = (char)('0' + (uv % 10ULL));
        uv /= 10ULL;
    }
    sb_reserve(sb, (uint32_t)pos);
    while (pos > 0) {
        sb->data[sb->len++] = tmp[--pos];
    }
    sb->data[sb->len] = '\0';
}

static inline void sb_put_json_strn(strbuf_t *sb, const char *s, uint32_t n) {
    sb_putc(sb, '"');
    static const char hex[] = "0123456789abcdef";
    for (uint32_t i = 0; i < n; i++) {
        unsigned char c = (unsigned char)s[i];
        switch (c) {
            case '"':  sb_puts(sb, "\\\""); break;
            case '\\': sb_puts(sb, "\\\\"); break;
            case '\b': sb_puts(sb, "\\b"); break;
            case '\f': sb_puts(sb, "\\f"); break;
            case '\n': sb_puts(sb, "\\n"); break;
            case '\r': sb_puts(sb, "\\r"); break;
            case '\t': sb_puts(sb, "\\t"); break;
            default:
                if (c < 0x20) {
                    sb_puts(sb, "\\u00");
                    sb_putc(sb, hex[(c >> 4) & 0xF]);
                    sb_putc(sb, hex[c & 0xF]);
                } else {
                    sb_putc(sb, (char)c);
                }
                break;
        }
    }
    sb_putc(sb, '"');
}

static inline void sb_put_json_str(strbuf_t *sb, const char *s) {
    if (!s) {
        sb_puts(sb, "\"\"");
        return;
    }
    sb_put_json_strn(sb, s, (uint32_t)xstrlen(s));
}

static inline void sb_put_b64(strbuf_t *sb, const uint8_t *data, uint32_t len) {
    static const char tbl[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    sb_putc(sb, '"');
    uint32_t i = 0;
    while (i + 3u <= len) {
        uint32_t v = ((uint32_t)data[i] << 16) | ((uint32_t)data[i + 1] << 8) | (uint32_t)data[i + 2];
        sb_putc(sb, tbl[(v >> 18) & 0x3F]);
        sb_putc(sb, tbl[(v >> 12) & 0x3F]);
        sb_putc(sb, tbl[(v >> 6) & 0x3F]);
        sb_putc(sb, tbl[v & 0x3F]);
        i += 3u;
    }
    uint32_t rem = len - i;
    if (rem == 1u) {
        uint32_t v = (uint32_t)data[i] << 16;
        sb_putc(sb, tbl[(v >> 18) & 0x3F]);
        sb_putc(sb, tbl[(v >> 12) & 0x3F]);
        sb_putc(sb, '=');
        sb_putc(sb, '=');
    } else if (rem == 2u) {
        uint32_t v = ((uint32_t)data[i] << 16) | ((uint32_t)data[i + 1] << 8);
        sb_putc(sb, tbl[(v >> 18) & 0x3F]);
        sb_putc(sb, tbl[(v >> 12) & 0x3F]);
        sb_putc(sb, tbl[(v >> 6) & 0x3F]);
        sb_putc(sb, '=');
    }
    sb_putc(sb, '"');
}

static inline int b64_val(unsigned char c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '+' || c == '-') return 62;
    if (c == '/' || c == '_') return 63;
    return -1;
}

static inline uint8_t *b64_decode(const char *src, uint32_t slen, uint32_t *out_len) {
    uint32_t max_out = (slen * 3u) / 4u + 4u;
    uint8_t *out = (uint8_t *)arena_alloc(max_out);
    uint32_t oi = 0;
    uint32_t acc = 0;
    int bits = 0;
    for (uint32_t i = 0; i < slen; i++) {
        unsigned char c = (unsigned char)src[i];
        if (c == '=' || c == ' ' || c == '\n' || c == '\r' || c == '\t') continue;
        int v = b64_val(c);
        if (v < 0) continue;
        acc = (acc << 6) | (uint32_t)v;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out[oi++] = (uint8_t)((acc >> bits) & 0xFFu);
        }
    }
    out[oi] = '\0';
    if (out_len) *out_len = oi;
    return out;
}

static inline uint64_t pack_slice(const void *ptr, uint32_t len) {
    return (((uint64_t)(uintptr_t)ptr) << 32) | (uint64_t)len;
}

static inline uint64_t pack_sb(const strbuf_t *sb) {
    return pack_slice(sb->data, sb->len);
}

/* ---- Lightweight Recursive-Descent JSON Parser ---- */

typedef enum {
    JK_NULL = 0,
    JK_BOOL,
    JK_NUM,
    JK_STR,
    JK_ARR,
    JK_OBJ
} jkind_t;

typedef struct jval_s jval_t;

typedef struct {
    const char *key;
    uint32_t klen;
    jval_t *val;
} jkv_t;

struct jval_s {
    jkind_t kind;
    bool bval;
    int64_t ival;
    const char *str; /* for JK_STR: decoded UTF-8; for JK_NUM: exact token */
    uint32_t len;
    jval_t **items;
    jkv_t *kvs;
    uint32_t count;
};

typedef struct {
    const char *p;
    const char *end;
    int depth;
} jparser_t;

static inline void jp_ws(jparser_t *jp) {
    while (jp->p < jp->end) {
        char c = *jp->p;
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
            jp->p++;
        } else {
            break;
        }
    }
}

static inline int hex_digit(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static inline bool jp_hex4(jparser_t *jp, uint32_t *out) {
    if (jp->end - jp->p < 4) return false;
    uint32_t v = 0;
    for (int i = 0; i < 4; i++) {
        int d = hex_digit(jp->p[i]);
        if (d < 0) return false;
        v = (v << 4) | (uint32_t)d;
    }
    jp->p += 4;
    *out = v;
    return true;
}

static inline void sb_put_utf8(strbuf_t *sb, uint32_t cp) {
    if (cp <= 0x7Fu) {
        sb_putc(sb, (char)cp);
    } else if (cp <= 0x7FFu) {
        sb_putc(sb, (char)(0xC0u | (cp >> 6)));
        sb_putc(sb, (char)(0x80u | (cp & 0x3Fu)));
    } else if (cp <= 0xFFFFu) {
        sb_putc(sb, (char)(0xE0u | (cp >> 12)));
        sb_putc(sb, (char)(0x80u | ((cp >> 6) & 0x3Fu)));
        sb_putc(sb, (char)(0x80u | (cp & 0x3Fu)));
    } else if (cp <= 0x10FFFFu) {
        sb_putc(sb, (char)(0xF0u | (cp >> 18)));
        sb_putc(sb, (char)(0x80u | ((cp >> 12) & 0x3Fu)));
        sb_putc(sb, (char)(0x80u | ((cp >> 6) & 0x3Fu)));
        sb_putc(sb, (char)(0x80u | (cp & 0x3Fu)));
    }
}

static inline bool jp_parse_raw_string(jparser_t *jp, const char **out_s, uint32_t *out_len) {
    if (jp->p >= jp->end || *jp->p != '"') return false;
    jp->p++;
    const char *start = jp->p;
    bool has_esc = false;
    while (jp->p < jp->end && *jp->p != '"') {
        if (*jp->p == '\\') {
            has_esc = true;
            jp->p++;
            if (jp->p >= jp->end) return false;
        }
        jp->p++;
    }
    if (jp->p >= jp->end || *jp->p != '"') return false;
    const char *finish = jp->p;
    jp->p++;
    if (!has_esc) {
        uint32_t n = (uint32_t)(finish - start);
        *out_s = arena_strndup(start, n);
        *out_len = n;
        return true;
    }
    strbuf_t sb;
    sb_init(&sb, (uint32_t)(finish - start) + 16u);
    jparser_t sub = {start, finish, 0};
    while (sub.p < sub.end) {
        char c = *sub.p++;
        if (c != '\\') {
            sb_putc(&sb, c);
            continue;
        }
        if (sub.p >= sub.end) return false;
        char e = *sub.p++;
        switch (e) {
            case '"': sb_putc(&sb, '"'); break;
            case '\\': sb_putc(&sb, '\\'); break;
            case '/': sb_putc(&sb, '/'); break;
            case 'b': sb_putc(&sb, '\b'); break;
            case 'f': sb_putc(&sb, '\f'); break;
            case 'n': sb_putc(&sb, '\n'); break;
            case 'r': sb_putc(&sb, '\r'); break;
            case 't': sb_putc(&sb, '\t'); break;
            case 'u': {
                uint32_t u1 = 0;
                if (!jp_hex4(&sub, &u1)) return false;
                if (u1 >= 0xD800u && u1 <= 0xDBFFu && sub.end - sub.p >= 6 && sub.p[0] == '\\' && sub.p[1] == 'u') {
                    const char *saved = sub.p;
                    sub.p += 2;
                    uint32_t u2 = 0;
                    if (jp_hex4(&sub, &u2) && u2 >= 0xDC00u && u2 <= 0xDFFFu) {
                        uint32_t cp = 0x10000u + (((u1 - 0xD800u) << 10) | (u2 - 0xDC00u));
                        sb_put_utf8(&sb, cp);
                    } else {
                        sub.p = saved;
                        sb_put_utf8(&sb, u1);
                    }
                } else {
                    sb_put_utf8(&sb, u1);
                }
                break;
            }
            default:
                sb_putc(&sb, e);
                break;
        }
    }
    *out_s = sb.data;
    *out_len = sb.len;
    return true;
}

static jval_t *jp_parse_val(jparser_t *jp);

static inline jval_t *jp_parse_obj(jparser_t *jp) {
    jp->p++; /* '{' */
    jp_ws(jp);
    jval_t *v = (jval_t *)arena_alloc(sizeof(jval_t));
    v->kind = JK_OBJ;
    if (jp->p < jp->end && *jp->p == '}') {
        jp->p++;
        return v;
    }
    uint32_t cap = 8, cnt = 0;
    jkv_t *kvs = (jkv_t *)arena_alloc(sizeof(jkv_t) * cap);
    while (jp->p < jp->end) {
        jp_ws(jp);
        const char *k = NULL;
        uint32_t kl = 0;
        if (!jp_parse_raw_string(jp, &k, &kl)) return NULL;
        jp_ws(jp);
        if (jp->p >= jp->end || *jp->p != ':') return NULL;
        jp->p++;
        jval_t *val = jp_parse_val(jp);
        if (!val) return NULL;
        if (cnt == cap) {
            cap *= 2u;
            jkv_t *nkvs = (jkv_t *)arena_alloc(sizeof(jkv_t) * cap);
            xmemcpy(nkvs, kvs, sizeof(jkv_t) * cnt);
            kvs = nkvs;
        }
        kvs[cnt].key = k;
        kvs[cnt].klen = kl;
        kvs[cnt].val = val;
        cnt++;
        jp_ws(jp);
        if (jp->p < jp->end && *jp->p == ',') {
            jp->p++;
            continue;
        }
        if (jp->p < jp->end && *jp->p == '}') {
            jp->p++;
            v->kvs = kvs;
            v->count = cnt;
            return v;
        }
        return NULL;
    }
    return NULL;
}

static inline jval_t *jp_parse_arr(jparser_t *jp) {
    jp->p++; /* '[' */
    jp_ws(jp);
    jval_t *v = (jval_t *)arena_alloc(sizeof(jval_t));
    v->kind = JK_ARR;
    if (jp->p < jp->end && *jp->p == ']') {
        jp->p++;
        return v;
    }
    uint32_t cap = 8, cnt = 0;
    jval_t **items = (jval_t **)arena_alloc(sizeof(jval_t *) * cap);
    while (jp->p < jp->end) {
        jval_t *el = jp_parse_val(jp);
        if (!el) return NULL;
        if (cnt == cap) {
            cap *= 2u;
            jval_t **nitems = (jval_t **)arena_alloc(sizeof(jval_t *) * cap);
            xmemcpy(nitems, items, sizeof(jval_t *) * cnt);
            items = nitems;
        }
        items[cnt++] = el;
        jp_ws(jp);
        if (jp->p < jp->end && *jp->p == ',') {
            jp->p++;
            continue;
        }
        if (jp->p < jp->end && *jp->p == ']') {
            jp->p++;
            v->items = items;
            v->count = cnt;
            return v;
        }
        return NULL;
    }
    return NULL;
}

static jval_t *jp_parse_val(jparser_t *jp) {
    if (jp->depth > 64) return NULL;
    jp->depth++;
    jp_ws(jp);
    jval_t *res = NULL;
    if (jp->p < jp->end) {
        char c = *jp->p;
        if (c == '{') {
            res = jp_parse_obj(jp);
        } else if (c == '[') {
            res = jp_parse_arr(jp);
        } else if (c == '"') {
            const char *s = NULL;
            uint32_t sl = 0;
            if (jp_parse_raw_string(jp, &s, &sl)) {
                res = (jval_t *)arena_alloc(sizeof(jval_t));
                res->kind = JK_STR;
                res->str = s;
                res->len = sl;
            }
        } else if (c == 't' && jp->end - jp->p >= 4 && xmemcmp(jp->p, "true", 4) == 0) {
            jp->p += 4;
            res = (jval_t *)arena_alloc(sizeof(jval_t));
            res->kind = JK_BOOL;
            res->bval = true;
        } else if (c == 'f' && jp->end - jp->p >= 5 && xmemcmp(jp->p, "false", 5) == 0) {
            jp->p += 5;
            res = (jval_t *)arena_alloc(sizeof(jval_t));
            res->kind = JK_BOOL;
            res->bval = false;
        } else if (c == 'n' && jp->end - jp->p >= 4 && xmemcmp(jp->p, "null", 4) == 0) {
            jp->p += 4;
            res = (jval_t *)arena_alloc(sizeof(jval_t));
            res->kind = JK_NULL;
        } else if (c == '-' || (c >= '0' && c <= '9')) {
            const char *start = jp->p;
            bool neg = false;
            if (*jp->p == '-') {
                neg = true;
                jp->p++;
            }
            int64_t acc = 0;
            while (jp->p < jp->end && *jp->p >= '0' && *jp->p <= '9') {
                acc = acc * 10 + (*jp->p - '0');
                jp->p++;
            }
            if (jp->p < jp->end && *jp->p == '.') {
                jp->p++;
                while (jp->p < jp->end && *jp->p >= '0' && *jp->p <= '9') jp->p++;
            }
            if (jp->p < jp->end && (*jp->p == 'e' || *jp->p == 'E')) {
                jp->p++;
                if (jp->p < jp->end && (*jp->p == '+' || *jp->p == '-')) jp->p++;
                while (jp->p < jp->end && *jp->p >= '0' && *jp->p <= '9') jp->p++;
            }
            uint32_t nl = (uint32_t)(jp->p - start);
            if (nl > 0) {
                res = (jval_t *)arena_alloc(sizeof(jval_t));
                res->kind = JK_NUM;
                res->str = arena_strndup(start, nl);
                res->len = nl;
                res->ival = neg ? -acc : acc;
            }
        }
    }
    jp->depth--;
    return res;
}

static inline jval_t *json_parse(const char *buf, uint32_t len) {
    if (!buf || len == 0) return NULL;
    jparser_t jp = {buf, buf + len, 0};
    return jp_parse_val(&jp);
}

static inline jval_t *jobj_get(const jval_t *obj, const char *key) {
    if (!obj || obj->kind != JK_OBJ || !key) return NULL;
    for (uint32_t i = 0; i < obj->count; i++) {
        if (xstreq_n(obj->kvs[i].key, obj->kvs[i].klen, key)) {
            return obj->kvs[i].val;
        }
    }
    return NULL;
}

static inline const char *jobj_str(const jval_t *obj, const char *key, uint32_t *out_len) {
    jval_t *v = jobj_get(obj, key);
    if (!v || v->kind != JK_STR) {
        if (out_len) *out_len = 0;
        return NULL;
    }
    if (out_len) *out_len = v->len;
    return v->str;
}

static inline int64_t jobj_i64(const jval_t *obj, const char *key, int64_t defv) {
    jval_t *v = jobj_get(obj, key);
    if (!v || v->kind != JK_NUM) return defv;
    return v->ival;
}

static inline bool jobj_bool(const jval_t *obj, const char *key, bool defv) {
    jval_t *v = jobj_get(obj, key);
    if (!v || v->kind != JK_BOOL) return defv;
    return v->bval;
}

static inline jval_t *jarr_get(const jval_t *arr, uint32_t idx) {
    if (!arr || arr->kind != JK_ARR || idx >= arr->count) return NULL;
    return arr->items[idx];
}

static inline uint8_t *jobj_b64(const jval_t *obj, const char *key, uint32_t *out_len) {
    uint32_t slen = 0;
    const char *s = jobj_str(obj, key, &slen);
    if (!s || slen == 0) {
        if (out_len) *out_len = 0;
        return NULL;
    }
    return b64_decode(s, slen, out_len);
}

static inline void json_write_sb(strbuf_t *sb, const jval_t *v) {
    if (!v) {
        sb_puts(sb, "null");
        return;
    }
    switch (v->kind) {
        case JK_NULL: sb_puts(sb, "null"); break;
        case JK_BOOL: sb_puts(sb, v->bval ? "true" : "false"); break;
        case JK_NUM:  sb_putn(sb, v->str, v->len); break;
        case JK_STR:  sb_put_json_strn(sb, v->str, v->len); break;
        case JK_ARR:
            sb_putc(sb, '[');
            for (uint32_t i = 0; i < v->count; i++) {
                if (i > 0) sb_putc(sb, ',');
                json_write_sb(sb, v->items[i]);
            }
            sb_putc(sb, ']');
            break;
        case JK_OBJ:
            sb_putc(sb, '{');
            for (uint32_t i = 0; i < v->count; i++) {
                if (i > 0) sb_putc(sb, ',');
                sb_put_json_strn(sb, v->kvs[i].key, v->kvs[i].klen);
                sb_putc(sb, ':');
                json_write_sb(sb, v->kvs[i].val);
            }
            sb_putc(sb, '}');
            break;
    }
}

static inline int64_t parse_i64_str(const char *s, uint32_t len, int64_t defv) {
    if (!s || len == 0) return defv;
    uint32_t i = 0;
    while (i < len && (s[i] == ' ' || s[i] == '\t')) i++;
    if (i >= len || s[i] < '0' || s[i] > '9') return defv;
    int64_t v = 0;
    while (i < len && s[i] >= '0' && s[i] <= '9') {
        v = v * 10 + (s[i] - '0');
        i++;
    }
    return v;
}

static inline void sb_put_joined_url(strbuf_t *sb, const char *base, uint32_t blen, const char *path, uint32_t plen) {
    while (blen > 0 && base[blen - 1] == '/') blen--;
    while (plen > 0 && *path == '/') {
        path++;
        plen--;
    }
    sb_putn(sb, base, blen);
    sb_putc(sb, '/');
    sb_putn(sb, path, plen);
}

static inline const char *get_config_base_url(const jval_t *cfg, const char *fallback, uint32_t *out_len) {
    const char *u = jobj_str(cfg, "endpoint_base_url", out_len);
    if (u && *out_len > 0) return u;
    u = jobj_str(cfg, "base_url", out_len);
    if (u && *out_len > 0) return u;
    *out_len = (uint32_t)xstrlen(fallback);
    return fallback;
}

static inline const char *get_sanitized_header(const jval_t *headers, const char *name, uint32_t *out_len) {
    if (!headers || headers->kind != JK_OBJ) {
        if (out_len) *out_len = 0;
        return NULL;
    }
    for (uint32_t i = 0; i < headers->count; i++) {
        if (xstrieq_n(headers->kvs[i].key, headers->kvs[i].klen, name) &&
            headers->kvs[i].val && headers->kvs[i].val->kind == JK_STR) {
            if (out_len) *out_len = headers->kvs[i].val->len;
            return headers->kvs[i].val->str;
        }
    }
    if (out_len) *out_len = 0;
    return NULL;
}

/* Redacts accidental key-shaped prefixes (sk-..., AIza...) from public error messages. */
static inline const char *sanitize_public_error_msg(const char *msg, uint32_t len, const char *fallback) {
    if (!msg || len == 0) return fallback;
    if (xstrfind(msg, len, "sk-") >= 0 || xstrfind(msg, len, "AIza") >= 0 ||
        xstrfind(msg, len, "Bearer ") >= 0 || xstrfind(msg, len, "CANARY") >= 0) {
        return fallback;
    }
    return arena_strndup(msg, len);
}

#endif
