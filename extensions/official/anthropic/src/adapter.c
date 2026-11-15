#include "common.h"

static void append_anth_header_cfg(strbuf_t *sb, const jval_t *cfg) {
    uint32_t vlen = 0;
    const char *ver = jobj_str(cfg, "anthropic_version", &vlen);
    if (!ver || vlen == 0) {
        ver = "2023-06-01";
        vlen = 10;
    }
    sb_puts(sb, "\"anthropic-version\":");
    sb_put_json_strn(sb, ver, vlen);
    uint32_t blen = 0;
    const char *beta = jobj_str(cfg, "anthropic_beta", &blen);
    if (beta && blen > 0) {
        sb_puts(sb, ",\"anthropic-beta\":");
        sb_put_json_strn(sb, beta, blen);
    }
}

static void emit_anth_cred_inj(strbuf_t *sb) {
    sb_puts(sb, "[{\"secret_slot_name\":\"api_key\",\"target_location\":\"CREDENTIAL_TARGET_LOCATION_HEADER\",\"field_name\":\"x-api-key\"}]");
}

static const char *map_anth_stop_reason(const char *s, uint32_t len, bool has_tools) {
    if (s && len > 0) {
        if (xstreq_n(s, len, "end_turn") || xstreq_n(s, len, "stop_sequence") || xstreq_n(s, len, "pause_turn")) {
            return has_tools ? "tool_calls" : "stop";
        }
        if (xstreq_n(s, len, "max_tokens")) return "length";
        if (xstreq_n(s, len, "tool_use")) return "tool_calls";
        if (xstreq_n(s, len, "refusal")) return "content_filter";
    }
    return has_tools ? "tool_calls" : "stop";
}

typedef struct {
    const jval_t **items;
    uint32_t count;
    uint32_t cap;
} blob_list_t;

static const jval_t *find_blob_ref(const jval_t *blob_refs, const char *uri, uint32_t ulen) {
    if (!blob_refs || blob_refs->kind != JK_ARR) return NULL;
    for (uint32_t i = 0; i < blob_refs->count; i++) {
        const jval_t *b = blob_refs->items[i];
        uint32_t bl = 0;
        const char *bu = jobj_str(b, "uri", &bl);
        if (bu && bl == ulen && xmemcmp(bu, uri, ulen) == 0) {
            return b;
        }
    }
    return NULL;
}

static void track_outbound_blob(blob_list_t *list, const jval_t *b) {
    if (!b) return;
    uint32_t ulen = 0;
    const char *uri = jobj_str(b, "uri", &ulen);
    if (!uri) return;
    for (uint32_t i = 0; i < list->count; i++) {
        uint32_t il = 0;
        const char *iu = jobj_str(list->items[i], "uri", &il);
        if (iu && il == ulen && xmemcmp(iu, uri, ulen) == 0) return;
    }
    if (list->count == list->cap) {
        uint32_t ncap = list->cap ? list->cap * 2u : 8u;
        const jval_t **ni = (const jval_t **)arena_alloc(sizeof(jval_t *) * ncap);
        if (list->count > 0) xmemcpy(ni, list->items, sizeof(jval_t *) * list->count);
        list->items = ni;
        list->cap = ncap;
    }
    list->items[list->count++] = b;
}

static uint32_t trim_anth_base_v1(const char *base, uint32_t blen) {
    while (blen > 0 && base[blen - 1] == '/') blen--;
    if (blen >= 3 && xstreq_n(base + blen - 3, 3, "/v1")) {
        blen -= 3;
        while (blen > 0 && base[blen - 1] == '/') blen--;
    }
    return blen;
}

static void emit_media_url_block(strbuf_t *body, const char *url, uint32_t ulen, const char *mime_hint, uint32_t mlen,
                                 bool prefer_inline_base64, const jval_t *blob_refs, blob_list_t *out_blobs) {
    if (xstrstarts(url, ulen, "data:")) {
        const char *semi = NULL;
        const char *comma = NULL;
        for (uint32_t i = 5; i < ulen; i++) {
            if (url[i] == ';' && !semi) semi = url + i;
            if (url[i] == ',') { comma = url + i; break; }
        }
        if (comma && comma > url + 5) {
            const char *mime = url + 5;
            uint32_t ml = semi && semi < comma ? (uint32_t)(semi - mime) : (uint32_t)(comma - mime);
            const char *b64 = comma + 1;
            uint32_t b64l = (uint32_t)((url + ulen) - b64);
            bool is_pdf = xstrieq_n(mime, ml, "application/pdf");
            sb_puts(body, is_pdf ? "{\"type\":\"document\",\"source\":{\"type\":\"base64\",\"media_type\":"
                                 : "{\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":");
            sb_put_json_strn(body, mime, ml);
            sb_puts(body, ",\"data\":");
            sb_put_json_strn(body, b64, b64l);
            sb_puts(body, "}}");
            return;
        }
    }
    if (xstrstarts(url, ulen, "blob_ref://")) {
        const char *mime = (mime_hint && mlen > 0) ? mime_hint : "image/png";
        uint32_t ml = (mime_hint && mlen > 0) ? mlen : 9;
        const jval_t *br = find_blob_ref(blob_refs, url, ulen);
        if (br) {
            track_outbound_blob(out_blobs, br);
            uint32_t bml = 0;
            const char *bm = jobj_str(br, "mime_type", &bml);
            if (bm && bml > 0 && !xstrieq_n(bm, bml, "application/octet-stream")) {
                mime = bm;
                ml = bml;
            }
        }
        bool is_pdf = xstrieq_n(mime, ml, "application/pdf");
        strbuf_t ph;
        sb_init(&ph, ulen + 16);
        sb_putn(&ph, url, ulen);
        sb_puts(&ph, "#raw_base64");
        sb_puts(body, is_pdf ? "{\"type\":\"document\",\"source\":{\"type\":\"base64\",\"media_type\":"
                             : "{\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":");
        sb_put_json_strn(body, mime, ml);
        sb_puts(body, ",\"data\":");
        sb_put_json_strn(body, ph.data, ph.len);
        sb_puts(body, "}}");
        return;
    }
    if (prefer_inline_base64 && !xstrstarts(url, ulen, "http://") && !xstrstarts(url, ulen, "https://")) {
        const char *mime = (mime_hint && mlen > 0) ? mime_hint : "application/pdf";
        uint32_t ml = (mime_hint && mlen > 0) ? mlen : 15;
        bool is_pdf = xstrieq_n(mime, ml, "application/pdf");
        sb_puts(body, is_pdf ? "{\"type\":\"document\",\"source\":{\"type\":\"base64\",\"media_type\":"
                             : "{\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":");
        sb_put_json_strn(body, mime, ml);
        sb_puts(body, ",\"data\":");
        sb_put_json_strn(body, url, ulen);
        sb_puts(body, "}}");
        return;
    }
    bool is_pdf = (mime_hint && xstrieq_n(mime_hint, mlen, "application/pdf")) ||
                  (ulen >= 4 && xstrieq_n(url + ulen - 4, 4, ".pdf"));
    sb_puts(body, is_pdf ? "{\"type\":\"document\",\"source\":{\"type\":\"url\",\"url\":"
                         : "{\"type\":\"image\",\"source\":{\"type\":\"url\",\"url\":");
    sb_put_json_strn(body, url, ulen);
    sb_puts(body, "}}");
}

static void emit_oai_msg_text(strbuf_t *dst, const jval_t *content) {
    if (!content) return;
    if (content->kind == JK_STR) {
        if (content->len > 0) {
            if (dst->len > 0) sb_putc(dst, '\n');
            sb_putn(dst, content->str, content->len);
        }
        return;
    }
    if (content->kind == JK_ARR) {
        for (uint32_t i = 0; i < content->count; i++) {
            const jval_t *p = content->items[i];
            uint32_t tl = 0;
            const char *t = jobj_str(p, "text", &tl);
            if (t && tl > 0) {
                if (dst->len > 0) sb_putc(dst, '\n');
                sb_putn(dst, t, tl);
            }
        }
    }
}

WASM_EXPORT("build_validation_probe")
uint64_t build_validation_probe(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t cfg_len = 0;
    const char *cfg_s = jobj_str(in, "config_json", &cfg_len);
    jval_t *cfg = json_parse(cfg_s, cfg_len);

    uint32_t blen = 0;
    const char *base = get_config_base_url(cfg, "https://api.anthropic.com", &blen);
    blen = trim_anth_base_v1(base, blen);

    strbuf_t sb;
    sb_init(&sb, 512);
    sb_puts(&sb, "{\"method\":\"GET\",\"target_url\":\"");
    sb_put_joined_url(&sb, base, blen, "v1/models", 9);
    sb_puts(&sb, "\",\"safe_headers\":{\"Accept\":\"application/json\",");
    append_anth_header_cfg(&sb, cfg);
    sb_puts(&sb, "},\"credential_injections\":");
    emit_anth_cred_inj(&sb);
    sb_putc(&sb, '}');
    return pack_sb(&sb);
}

WASM_EXPORT("verify_validation_result")
uint64_t verify_validation_result(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    int64_t status = jobj_i64(in, "http_status", 0);
    uint32_t rlen = 0;
    uint8_t *rbody = jobj_b64(in, "response_body", &rlen);

    strbuf_t sb;
    sb_init(&sb, 512);
    if (status >= 200 && status < 300) {
        jval_t *doc = json_parse((const char *)rbody, rlen);
        jval_t *data = jobj_get(doc, "data");
        sb_puts(&sb, "{\"valid\":true,\"detected_model_ids\":[");
        if (data && data->kind == JK_ARR) {
            bool first = true;
            for (uint32_t i = 0; i < data->count; i++) {
                uint32_t ml = 0;
                const char *mid = jobj_str(data->items[i], "id", &ml);
                if (mid && ml > 0) {
                    if (!first) sb_putc(&sb, ',');
                    sb_put_json_strn(&sb, mid, ml);
                    first = false;
                }
            }
        }
        sb_puts(&sb, "]}");
    } else {
        sb_puts(&sb, "{\"valid\":false,\"failure_code\":\"");
        sb_puts(&sb, (status == 401 || status == 403) ? "authentication_error" : "upstream_validation_failed");
        sb_puts(&sb, "\",\"sanitized_message\":\"Anthropic validation probe returned HTTP ");
        sb_put_i64(&sb, status);
        sb_puts(&sb, ".\"}");
    }
    return pack_sb(&sb);
}

WASM_EXPORT("transform_request")
uint64_t transform_request(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t cfg_len = 0;
    const char *cfg_s = jobj_str(in, "config_json", &cfg_len);
    jval_t *cfg = json_parse(cfg_s, cfg_len);

    uint32_t req_len = 0;
    uint8_t *req_raw = jobj_b64(in, "normalized_request_json", &req_len);
    jval_t *req = json_parse((const char *)req_raw, req_len);
    jval_t *blob_refs = jobj_get(in, "blob_refs");
    blob_list_t out_blobs = {0};

    strbuf_t body;
    sb_init(&body, 2048);
    sb_putc(&body, '{');

    uint32_t mlen = 0;
    const char *model = jobj_str(req, "model", &mlen);
    if (!model || mlen == 0) {
        model = "claude-sonnet-4-5";
        mlen = 17;
    }
    sb_puts(&body, "\"model\":");
    sb_put_json_strn(&body, model, mlen);

    int64_t max_tok = jobj_i64(req, "max_tokens", -1);
    if (max_tok <= 0) max_tok = jobj_i64(req, "max_completion_tokens", -1);
    if (max_tok <= 0) max_tok = 4096;
    sb_puts(&body, ",\"max_tokens\":");
    sb_put_i64(&body, max_tok);

    jval_t *temp = jobj_get(req, "temperature");
    if (temp && temp->kind == JK_NUM) {
        sb_puts(&body, ",\"temperature\":");
        sb_putn(&body, temp->str, temp->len);
    }
    jval_t *top_p = jobj_get(req, "top_p");
    if (top_p && top_p->kind == JK_NUM) {
        sb_puts(&body, ",\"top_p\":");
        sb_putn(&body, top_p->str, top_p->len);
    }
    bool stream = jobj_bool(req, "stream", false);
    if (stream) sb_puts(&body, ",\"stream\":true");

    jval_t *stop = jobj_get(req, "stop");
    if (stop) {
        if (stop->kind == JK_STR) {
            sb_puts(&body, ",\"stop_sequences\":[");
            sb_put_json_strn(&body, stop->str, stop->len);
            sb_putc(&body, ']');
        } else if (stop->kind == JK_ARR && stop->count > 0) {
            sb_puts(&body, ",\"stop_sequences\":");
            json_write_sb(&body, stop);
        }
    }

    /* Extract system/developer messages and build Anthropic messages */
    jval_t *msgs = jobj_get(req, "messages");
    strbuf_t sys_buf;
    sb_init(&sys_buf, 256);
    if (msgs && msgs->kind == JK_ARR) {
        for (uint32_t i = 0; i < msgs->count; i++) {
            const jval_t *m = msgs->items[i];
            uint32_t rlen = 0;
            const char *role = jobj_str(m, "role", &rlen);
            if (role && (xstreq_n(role, rlen, "system") || xstreq_n(role, rlen, "developer"))) {
                emit_oai_msg_text(&sys_buf, jobj_get(m, "content"));
            }
        }
    }
    if (sys_buf.len > 0) {
        sb_puts(&body, ",\"system\":");
        sb_put_json_strn(&body, sys_buf.data, sys_buf.len);
    }

    sb_puts(&body, ",\"messages\":[");
    if (msgs && msgs->kind == JK_ARR) {
        bool first_msg = true;
        uint32_t i = 0;
        while (i < msgs->count) {
            const jval_t *m = msgs->items[i];
            uint32_t rlen = 0;
            const char *role = jobj_str(m, "role", &rlen);
            if (!role || xstreq_n(role, rlen, "system") || xstreq_n(role, rlen, "developer")) {
                i++;
                continue;
            }
            if (xstreq_n(role, rlen, "tool")) {
                if (!first_msg) sb_putc(&body, ',');
                first_msg = false;
                sb_puts(&body, "{\"role\":\"user\",\"content\":[");
                bool first_b = true;
                while (i < msgs->count) {
                    const jval_t *tm = msgs->items[i];
                    uint32_t trl = 0;
                    const char *tr = jobj_str(tm, "role", &trl);
                    if (!tr || !xstreq_n(tr, trl, "tool")) break;
                    if (!first_b) sb_putc(&body, ',');
                    first_b = false;
                    uint32_t tidl = 0;
                    const char *tid = jobj_str(tm, "tool_call_id", &tidl);
                    strbuf_t tcb;
                    sb_init(&tcb, 128);
                    emit_oai_msg_text(&tcb, jobj_get(tm, "content"));
                    sb_puts(&body, "{\"type\":\"tool_result\",\"tool_use_id\":");
                    sb_put_json_strn(&body, tid ? tid : "", tidl);
                    sb_puts(&body, ",\"content\":");
                    sb_put_json_strn(&body, tcb.data, tcb.len);
                    sb_putc(&body, '}');
                    i++;
                }
                sb_puts(&body, "]}");
                continue;
            }

            if (!first_msg) sb_putc(&body, ',');
            first_msg = false;
            const char *anth_role = xstreq_n(role, rlen, "assistant") ? "assistant" : "user";
            sb_puts(&body, "{\"role\":\"");
            sb_puts(&body, anth_role);
            sb_puts(&body, "\",\"content\":[");

            bool first_blk = true;
            const jval_t *content = jobj_get(m, "content");
            if (content && content->kind == JK_STR && content->len > 0) {
                sb_puts(&body, "{\"type\":\"text\",\"text\":");
                sb_put_json_strn(&body, content->str, content->len);
                sb_putc(&body, '}');
                first_blk = false;
            } else if (content && content->kind == JK_ARR) {
                for (uint32_t pi = 0; pi < content->count; pi++) {
                    const jval_t *p = content->items[pi];
                    uint32_t ptl = 0;
                    const char *pt = jobj_str(p, "type", &ptl);
                    if (pt && (xstreq_n(pt, ptl, "text") || xstreq_n(pt, ptl, "input_text"))) {
                        uint32_t tl = 0;
                        const char *t = jobj_str(p, "text", &tl);
                        if (t) {
                            if (!first_blk) sb_putc(&body, ',');
                            first_blk = false;
                            sb_puts(&body, "{\"type\":\"text\",\"text\":");
                            sb_put_json_strn(&body, t, tl);
                            sb_putc(&body, '}');
                        }
                    } else if (pt && (xstreq_n(pt, ptl, "image_url") || xstreq_n(pt, ptl, "input_image"))) {
                        jval_t *iu = jobj_get(p, "image_url");
                        uint32_t ul = 0;
                        const char *u = NULL;
                        if (iu && iu->kind == JK_OBJ) u = jobj_str(iu, "url", &ul);
                        else if (iu && iu->kind == JK_STR) { u = iu->str; ul = iu->len; }
                        else u = jobj_str(p, "url", &ul);
                        if (u && ul > 0) {
                            if (!first_blk) sb_putc(&body, ',');
                            first_blk = false;
                            emit_media_url_block(&body, u, ul, NULL, 0, false, blob_refs, &out_blobs);
                        }
                    } else if (pt && (xstreq_n(pt, ptl, "input_file") || xstreq_n(pt, ptl, "document") || xstreq_n(pt, ptl, "file"))) {
                        jval_t *fileobj = jobj_get(p, "file");
                        uint32_t fl = 0;
                        const char *fdata = NULL;
                        bool from_file_data = false;
                        if (fileobj && fileobj->kind == JK_OBJ) {
                            fdata = jobj_str(fileobj, "file_data", &fl);
                            if (fdata) from_file_data = true;
                            if (!fdata) fdata = jobj_str(fileobj, "file_url", &fl);
                            if (!fdata) fdata = jobj_str(fileobj, "url", &fl);
                        }
                        if (!fdata) {
                            fdata = jobj_str(p, "file_data", &fl);
                            if (fdata) from_file_data = true;
                        }
                        if (!fdata) fdata = jobj_str(p, "file_url", &fl);
                        if (!fdata) fdata = jobj_str(p, "url", &fl);
                        uint32_t ml = 0;
                        const char *mt = jobj_str(p, "mime_type", &ml);
                        if (!mt && fileobj && fileobj->kind == JK_OBJ) mt = jobj_str(fileobj, "mime_type", &ml);
                        if (!mt) { mt = "application/pdf"; ml = 15; }
                        if (fdata && fl > 0) {
                            if (!first_blk) sb_putc(&body, ',');
                            first_blk = false;
                            emit_media_url_block(&body, fdata, fl, mt, ml, from_file_data, blob_refs, &out_blobs);
                        }
                    }
                }
            }

            const jval_t *tcalls = jobj_get(m, "tool_calls");
            if (tcalls && tcalls->kind == JK_ARR) {
                for (uint32_t ti = 0; ti < tcalls->count; ti++) {
                    const jval_t *tc = tcalls->items[ti];
                    uint32_t tcidl = 0;
                    const char *tcid = jobj_str(tc, "id", &tcidl);
                    const jval_t *fn = jobj_get(tc, "function");
                    uint32_t fnl = 0, fal = 0;
                    const char *fname = jobj_str(fn ? fn : tc, "name", &fnl);
                    const char *fargs = jobj_str(fn ? fn : tc, "arguments", &fal);
                    jval_t *parsed_args = (fargs && fal > 0) ? json_parse(fargs, fal) : NULL;
                    if (!first_blk) sb_putc(&body, ',');
                    first_blk = false;
                    sb_puts(&body, "{\"type\":\"tool_use\",\"id\":");
                    sb_put_json_strn(&body, tcid ? tcid : "call_0", tcid ? tcidl : 6);
                    sb_puts(&body, ",\"name\":");
                    sb_put_json_strn(&body, fname ? fname : "", fnl);
                    sb_puts(&body, ",\"input\":");
                    if (parsed_args && parsed_args->kind == JK_OBJ) {
                        json_write_sb(&body, parsed_args);
                    } else {
                        sb_puts(&body, "{}");
                    }
                    sb_putc(&body, '}');
                }
            }
            if (first_blk) {
                sb_puts(&body, "{\"type\":\"text\",\"text\":\"\"}");
            }
            sb_puts(&body, "]}");
            i++;
        }
    }
    sb_putc(&body, ']');

    jval_t *tools = jobj_get(req, "tools");
    if (tools && tools->kind == JK_ARR && tools->count > 0) {
        sb_puts(&body, ",\"tools\":[");
        bool first_t = true;
        for (uint32_t i = 0; i < tools->count; i++) {
            const jval_t *t = tools->items[i];
            const jval_t *fn = jobj_get(t, "function");
            const jval_t *src = fn ? fn : t;
            uint32_t nl = 0, dl = 0;
            const char *name = jobj_str(src, "name", &nl);
            if (!name || nl == 0) continue;
            const char *desc = jobj_str(src, "description", &dl);
            const jval_t *params = jobj_get(src, "parameters");
            if (!params) params = jobj_get(src, "input_schema");
            if (!first_t) sb_putc(&body, ',');
            first_t = false;
            sb_puts(&body, "{\"name\":");
            sb_put_json_strn(&body, name, nl);
            if (desc && dl > 0) {
                sb_puts(&body, ",\"description\":");
                sb_put_json_strn(&body, desc, dl);
            }
            sb_puts(&body, ",\"input_schema\":");
            if (params && params->kind == JK_OBJ) {
                json_write_sb(&body, params);
            } else {
                sb_puts(&body, "{\"type\":\"object\",\"properties\":{}}");
            }
            sb_putc(&body, '}');
        }
        sb_putc(&body, ']');
    }

    jval_t *tc_choice = jobj_get(req, "tool_choice");
    if (tc_choice) {
        if (tc_choice->kind == JK_STR) {
            if (xstreq_n(tc_choice->str, tc_choice->len, "auto")) {
                sb_puts(&body, ",\"tool_choice\":{\"type\":\"auto\"}");
            } else if (xstreq_n(tc_choice->str, tc_choice->len, "required") || xstreq_n(tc_choice->str, tc_choice->len, "any")) {
                sb_puts(&body, ",\"tool_choice\":{\"type\":\"any\"}");
            } else if (xstreq_n(tc_choice->str, tc_choice->len, "none")) {
                sb_puts(&body, ",\"tool_choice\":{\"type\":\"none\"}");
            }
        } else if (tc_choice->kind == JK_OBJ) {
            const jval_t *fn = jobj_get(tc_choice, "function");
            uint32_t nl = 0;
            const char *name = jobj_str(fn ? fn : tc_choice, "name", &nl);
            if (name && nl > 0) {
                sb_puts(&body, ",\"tool_choice\":{\"type\":\"tool\",\"name\":");
                sb_put_json_strn(&body, name, nl);
                sb_putc(&body, '}');
            }
        }
    }
    sb_putc(&body, '}');

    uint32_t blen = 0;
    const char *base = get_config_base_url(cfg, "https://api.anthropic.com", &blen);
    blen = trim_anth_base_v1(base, blen);

    strbuf_t spec;
    sb_init(&spec, 2048 + body.len);
    sb_puts(&spec, "{\"method\":\"POST\",\"target_url\":\"");
    sb_put_joined_url(&spec, base, blen, "v1/messages", 11);
    sb_puts(&spec, "\",\"safe_headers\":{\"Content-Type\":\"application/json\",\"Accept\":\"application/json\",");
    append_anth_header_cfg(&spec, cfg);
    sb_puts(&spec, "},\"body_bytes\":");
    sb_put_b64(&spec, (const uint8_t *)body.data, body.len);
    if (out_blobs.count > 0) {
        sb_puts(&spec, ",\"outbound_blob_splices\":[");
        for (uint32_t i = 0; i < out_blobs.count; i++) {
            if (i > 0) sb_putc(&spec, ',');
            json_write_sb(&spec, out_blobs.items[i]);
        }
        sb_putc(&spec, ']');
    }
    sb_puts(&spec, ",\"credential_injections\":");
    emit_anth_cred_inj(&spec);
    sb_putc(&spec, '}');
    return pack_sb(&spec);
}

static void emit_usage_json(strbuf_t *sb, int64_t in_tok, int64_t out_tok, int64_t cache_r, int64_t cache_w) {
    sb_puts(sb, "{\"input_tokens\":");
    sb_put_i64(sb, in_tok);
    sb_puts(sb, ",\"output_tokens\":");
    sb_put_i64(sb, out_tok);
    sb_puts(sb, ",\"cache_read_tokens\":");
    sb_put_i64(sb, cache_r);
    sb_puts(sb, ",\"cache_write_tokens\":");
    sb_put_i64(sb, cache_w);
    sb_puts(sb, ",\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_EXACT\"}");
}

WASM_EXPORT("transform_response")
uint64_t transform_response(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t rlen = 0;
    uint8_t *raw = jobj_b64(in, "raw_response_body", &rlen);
    jval_t *doc = json_parse((const char *)raw, rlen);
    if (!doc || doc->kind != JK_OBJ) {
        strbuf_t empty_out;
        sb_init(&empty_out, 64);
        sb_puts(&empty_out, "{\"normalized_response_json\":null}");
        return pack_sb(&empty_out);
    }

    uint32_t idl = 0, ml = 0, srl = 0;
    const char *id = jobj_str(doc, "id", &idl);
    if (!id || idl == 0) { id = "msg_anth"; idl = 8; }
    const char *model = jobj_str(doc, "model", &ml);
    if (!model || ml == 0) { model = "claude"; ml = 6; }
    const char *sr = jobj_str(doc, "stop_reason", &srl);

    strbuf_t text_buf, tc_buf;
    sb_init(&text_buf, 512);
    sb_init(&tc_buf, 512);
    uint32_t tool_count = 0;

    jval_t *content = jobj_get(doc, "content");
    if (content && content->kind == JK_ARR) {
        for (uint32_t i = 0; i < content->count; i++) {
            const jval_t *b = content->items[i];
            uint32_t btl = 0;
            const char *bt = jobj_str(b, "type", &btl);
            if (bt && xstreq_n(bt, btl, "text")) {
                uint32_t tl = 0;
                const char *txt = jobj_str(b, "text", &tl);
                if (txt && tl > 0) sb_putn(&text_buf, txt, tl);
            } else if (bt && xstreq_n(bt, btl, "tool_use")) {
                uint32_t tidl = 0, fnl = 0;
                const char *tid = jobj_str(b, "id", &tidl);
                const char *fn = jobj_str(b, "name", &fnl);
                const jval_t *inp = jobj_get(b, "input");
                strbuf_t args_sb;
                sb_init(&args_sb, 128);
                if (inp) json_write_sb(&args_sb, inp);
                else sb_puts(&args_sb, "{}");

                if (tool_count > 0) sb_putc(&tc_buf, ',');
                tool_count++;
                sb_puts(&tc_buf, "{\"id\":");
                sb_put_json_strn(&tc_buf, tid ? tid : "call_0", tid ? tidl : 6);
                sb_puts(&tc_buf, ",\"type\":\"function\",\"function\":{\"name\":");
                sb_put_json_strn(&tc_buf, fn ? fn : "", fnl);
                sb_puts(&tc_buf, ",\"arguments\":");
                sb_put_json_strn(&tc_buf, args_sb.data, args_sb.len);
                sb_puts(&tc_buf, "}}");
            }
        }
    }

    const char *finish = map_anth_stop_reason(sr, srl, tool_count > 0);
    jval_t *u = jobj_get(doc, "usage");
    int64_t in_tok = jobj_i64(u, "input_tokens", 0);
    int64_t out_tok = jobj_i64(u, "output_tokens", 0);
    int64_t cache_r = jobj_i64(u, "cache_read_input_tokens", 0);
    int64_t cache_w = jobj_i64(u, "cache_creation_input_tokens", 0);

    strbuf_t norm;
    sb_init(&norm, 1024 + text_buf.len + tc_buf.len);
    sb_puts(&norm, "{\"id\":");
    sb_put_json_strn(&norm, id, idl);
    sb_puts(&norm, ",\"object\":\"chat.completion\",\"model\":");
    sb_put_json_strn(&norm, model, ml);
    sb_puts(&norm, ",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":");
    sb_put_json_strn(&norm, text_buf.data, text_buf.len);
    if (tool_count > 0) {
        sb_puts(&norm, ",\"tool_calls\":[");
        sb_putn(&norm, tc_buf.data, tc_buf.len);
        sb_putc(&norm, ']');
    }
    sb_puts(&norm, "},\"finish_reason\":");
    sb_put_json_str(&norm, finish);
    sb_puts(&norm, "}],\"usage\":{\"prompt_tokens\":");
    sb_put_i64(&norm, in_tok);
    sb_puts(&norm, ",\"completion_tokens\":");
    sb_put_i64(&norm, out_tok);
    sb_puts(&norm, ",\"total_tokens\":");
    sb_put_i64(&norm, in_tok + out_tok);
    sb_puts(&norm, ",\"input_tokens\":");
    sb_put_i64(&norm, in_tok);
    sb_puts(&norm, ",\"output_tokens\":");
    sb_put_i64(&norm, out_tok);
    sb_puts(&norm, ",\"cache_read_tokens\":");
    sb_put_i64(&norm, cache_r);
    sb_puts(&norm, ",\"cache_write_tokens\":");
    sb_put_i64(&norm, cache_w);
    sb_puts(&norm, "}}");

    strbuf_t out;
    sb_init(&out, norm.len * 2 + 256);
    sb_puts(&out, "{\"normalized_response_json\":");
    sb_put_b64(&out, (const uint8_t *)norm.data, norm.len);
    sb_puts(&out, ",\"usage\":");
    emit_usage_json(&out, in_tok, out_tok, cache_r, cache_w);
    sb_puts(&out, ",\"upstream_model_id\":");
    sb_put_json_strn(&out, model, ml);
    sb_puts(&out, ",\"finish_reason\":");
    sb_put_json_str(&out, finish);
    sb_putc(&out, '}');
    return pack_sb(&out);
}

static void push_sse_frame(strbuf_t *frames_arr, bool *first_frame, const char *s, uint32_t n) {
    if (!*first_frame) sb_putc(frames_arr, ',');
    *first_frame = false;
    sb_put_b64(frames_arr, (const uint8_t *)s, n);
}

WASM_EXPORT("transform_stream_chunk")
uint64_t transform_stream_chunk(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t ssl = 0, rawl = 0;
    uint8_t *ss_raw = jobj_b64(in, "session_state", &ssl);
    uint8_t *chunk = jobj_b64(in, "raw_upstream_chunk", &rawl);
    bool is_eof = jobj_bool(in, "is_eof", false);

    jval_t *st = ssl > 0 ? json_parse((const char *)ss_raw, ssl) : NULL;
    uint32_t old_bl = 0, idl = 0, ml = 0, frl = 0;
    const char *old_buf = jobj_str(st, "buf", &old_bl);
    const char *id = jobj_str(st, "id", &idl);
    if (!id || idl == 0) { id = "msg_anth"; idl = 8; }
    const char *model = jobj_str(st, "model", &ml);
    if (!model || ml == 0) { model = "claude"; ml = 6; }
    const char *finish_r = jobj_str(st, "finish", &frl);
    bool sent_role = jobj_bool(st, "sent_role", false);
    int64_t in_tok = jobj_i64(st, "in", 0);
    int64_t out_tok = jobj_i64(st, "out", 0);
    int64_t cache_r = jobj_i64(st, "cr", 0);
    int64_t cache_w = jobj_i64(st, "cw", 0);
    int64_t tool_idx = jobj_i64(st, "ti", -1);
    bool completed = jobj_bool(st, "done", false);
    bool saw_usage = jobj_bool(st, "su", false);

    strbuf_t buf;
    sb_init(&buf, old_bl + rawl + 64);
    if (old_buf && old_bl > 0) sb_putn(&buf, old_buf, old_bl);
    if (chunk && rawl > 0) sb_putn(&buf, (const char *)chunk, rawl);

    strbuf_t frames;
    sb_init(&frames, 1024);
    sb_putc(&frames, '[');
    bool first_frame = true;

    uint32_t pos = 0;
    while (pos < buf.len) {
        uint32_t nl = pos;
        while (nl < buf.len && buf.data[nl] != '\n') nl++;
        if (nl == buf.len && !is_eof) {
            break; /* incomplete line waits in state.buf */
        }
        uint32_t line_end = nl;
        if (line_end > pos && buf.data[line_end - 1] == '\r') line_end--;
        const char *line = buf.data + pos;
        uint32_t llen = line_end - pos;
        pos = (nl < buf.len) ? (nl + 1) : buf.len;

        if (!xstrstarts(line, llen, "data:")) continue;
        const char *pay = line + 5;
        uint32_t plen = llen - 5;
        while (plen > 0 && (*pay == ' ' || *pay == '\t')) { pay++; plen--; }
        if (plen == 0) continue;
        if (xstreq_n(pay, plen, "[DONE]")) {
            if (!completed) {
                if (!finish_r || frl == 0) { finish_r = "stop"; frl = 4; }
                strbuf_t f;
                sb_init(&f, 128);
                sb_puts(&f, "data: {\"id\":");
                sb_put_json_strn(&f, id, idl);
                sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                sb_put_json_strn(&f, model, ml);
                sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":");
                sb_put_json_strn(&f, finish_r, frl);
                sb_puts(&f, "}]}\n\n");
                push_sse_frame(&frames, &first_frame, f.data, f.len);
                push_sse_frame(&frames, &first_frame, "data: [DONE]\n\n", 14);
                completed = true;
            }
            continue;
        }
        jval_t *ev = json_parse(pay, plen);
        if (!ev || ev->kind != JK_OBJ) continue;
        uint32_t etl = 0;
        const char *et = jobj_str(ev, "type", &etl);
        if (!et) continue;

        if (xstreq_n(et, etl, "message_start")) {
            const jval_t *msg = jobj_get(ev, "message");
            uint32_t nid_l = 0, nmod_l = 0;
            const char *nid = jobj_str(msg, "id", &nid_l);
            const char *nmod = jobj_str(msg, "model", &nmod_l);
            if (nid && nid_l > 0) { id = nid; idl = nid_l; }
            if (nmod && nmod_l > 0) { model = nmod; ml = nmod_l; }
            const jval_t *mu = jobj_get(msg, "usage");
            if (mu) {
                in_tok = jobj_i64(mu, "input_tokens", in_tok);
                cache_r = jobj_i64(mu, "cache_read_input_tokens", cache_r);
                cache_w = jobj_i64(mu, "cache_creation_input_tokens", cache_w);
                saw_usage = true;
            }
            if (!sent_role) {
                strbuf_t f;
                sb_init(&f, 256);
                sb_puts(&f, "data: {\"id\":");
                sb_put_json_strn(&f, id, idl);
                sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                sb_put_json_strn(&f, model, ml);
                sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n");
                push_sse_frame(&frames, &first_frame, f.data, f.len);
                sent_role = true;
            }
        } else if (xstreq_n(et, etl, "content_block_start")) {
            const jval_t *cb = jobj_get(ev, "content_block");
            uint32_t cbtl = 0;
            const char *cbt = jobj_str(cb, "type", &cbtl);
            if (cbt && xstreq_n(cbt, cbtl, "tool_use")) {
                tool_idx++;
                uint32_t tidl = 0, fnl = 0;
                const char *tid = jobj_str(cb, "id", &tidl);
                const char *fn = jobj_str(cb, "name", &fnl);
                strbuf_t f;
                sb_init(&f, 256);
                sb_puts(&f, "data: {\"id\":");
                sb_put_json_strn(&f, id, idl);
                sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                sb_put_json_strn(&f, model, ml);
                sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":");
                sb_put_i64(&f, tool_idx);
                sb_puts(&f, ",\"id\":");
                sb_put_json_strn(&f, tid ? tid : "call_0", tid ? tidl : 6);
                sb_puts(&f, ",\"type\":\"function\",\"function\":{\"name\":");
                sb_put_json_strn(&f, fn ? fn : "", fnl);
                sb_puts(&f, ",\"arguments\":\"\"}}]}}]}\n\n");
                push_sse_frame(&frames, &first_frame, f.data, f.len);
            }
        } else if (xstreq_n(et, etl, "content_block_delta")) {
            const jval_t *d = jobj_get(ev, "delta");
            uint32_t dtl = 0;
            const char *dt = jobj_str(d, "type", &dtl);
            if (dt && xstreq_n(dt, dtl, "text_delta")) {
                uint32_t tl = 0;
                const char *txt = jobj_str(d, "text", &tl);
                if (txt && tl > 0) {
                    strbuf_t f;
                    sb_init(&f, 256 + tl);
                    sb_puts(&f, "data: {\"id\":");
                    sb_put_json_strn(&f, id, idl);
                    sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                    sb_put_json_strn(&f, model, ml);
                    sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{\"content\":");
                    sb_put_json_strn(&f, txt, tl);
                    sb_puts(&f, "}}]}\n\n");
                    push_sse_frame(&frames, &first_frame, f.data, f.len);
                }
            } else if (dt && xstreq_n(dt, dtl, "thinking_delta")) {
                uint32_t tl = 0;
                const char *txt = jobj_str(d, "thinking", &tl);
                if (txt && tl > 0) {
                    strbuf_t f;
                    sb_init(&f, 256 + tl);
                    sb_puts(&f, "data: {\"id\":");
                    sb_put_json_strn(&f, id, idl);
                    sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                    sb_put_json_strn(&f, model, ml);
                    sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":");
                    sb_put_json_strn(&f, txt, tl);
                    sb_puts(&f, "}}]}\n\n");
                    push_sse_frame(&frames, &first_frame, f.data, f.len);
                }
            } else if (dt && xstreq_n(dt, dtl, "input_json_delta")) {
                uint32_t pl = 0;
                const char *pj = jobj_str(d, "partial_json", &pl);
                if (pj && pl > 0) {
                    int64_t tidx = tool_idx >= 0 ? tool_idx : 0;
                    strbuf_t f;
                    sb_init(&f, 256 + pl);
                    sb_puts(&f, "data: {\"id\":");
                    sb_put_json_strn(&f, id, idl);
                    sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
                    sb_put_json_strn(&f, model, ml);
                    sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":");
                    sb_put_i64(&f, tidx);
                    sb_puts(&f, ",\"function\":{\"arguments\":");
                    sb_put_json_strn(&f, pj, pl);
                    sb_puts(&f, "}}]}}]}\n\n");
                    push_sse_frame(&frames, &first_frame, f.data, f.len);
                }
            }
        } else if (xstreq_n(et, etl, "message_delta")) {
            const jval_t *d = jobj_get(ev, "delta");
            uint32_t srl = 0;
            const char *sr = jobj_str(d, "stop_reason", &srl);
            const char *mapped = map_anth_stop_reason(sr, srl, tool_idx >= 0);
            finish_r = mapped;
            frl = (uint32_t)xstrlen(mapped);
            const jval_t *du = jobj_get(ev, "usage");
            if (du) {
                in_tok = jobj_i64(du, "input_tokens", in_tok);
                out_tok = jobj_i64(du, "output_tokens", out_tok);
                cache_r = jobj_i64(du, "cache_read_input_tokens", cache_r);
                cache_w = jobj_i64(du, "cache_creation_input_tokens", cache_w);
                saw_usage = true;
            }
            strbuf_t f;
            sb_init(&f, 256);
            sb_puts(&f, "data: {\"id\":");
            sb_put_json_strn(&f, id, idl);
            sb_puts(&f, ",\"object\":\"chat.completion.chunk\",\"model\":");
            sb_put_json_strn(&f, model, ml);
            sb_puts(&f, ",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":");
            sb_put_json_strn(&f, finish_r, frl);
            sb_puts(&f, "}]}\n\n");
            push_sse_frame(&frames, &first_frame, f.data, f.len);
        } else if (xstreq_n(et, etl, "message_stop")) {
            if (!completed) {
                if (saw_usage) {
                    strbuf_t uf;
                    sb_init(&uf, 256);
                    sb_puts(&uf, "data: {\"id\":");
                    sb_put_json_strn(&uf, id, idl);
                    sb_puts(&uf, ",\"object\":\"chat.completion.chunk\",\"model\":");
                    sb_put_json_strn(&uf, model, ml);
                    sb_puts(&uf, ",\"choices\":[],\"usage\":{\"prompt_tokens\":");
                    sb_put_i64(&uf, in_tok);
                    sb_puts(&uf, ",\"completion_tokens\":");
                    sb_put_i64(&uf, out_tok);
                    sb_puts(&uf, ",\"total_tokens\":");
                    sb_put_i64(&uf, in_tok + out_tok);
                    sb_puts(&uf, ",\"input_tokens\":");
                    sb_put_i64(&uf, in_tok);
                    sb_puts(&uf, ",\"output_tokens\":");
                    sb_put_i64(&uf, out_tok);
                    sb_puts(&uf, ",\"cache_read_tokens\":");
                    sb_put_i64(&uf, cache_r);
                    sb_puts(&uf, ",\"cache_write_tokens\":");
                    sb_put_i64(&uf, cache_w);
                    sb_puts(&uf, "}}\n\n");
                    push_sse_frame(&frames, &first_frame, uf.data, uf.len);
                }
                push_sse_frame(&frames, &first_frame, "data: [DONE]\n\n", 14);
                completed = true;
            }
        }
    }

    sb_putc(&frames, ']');

    strbuf_t next_st;
    sb_init(&next_st, 256 + (buf.len - pos));
    sb_puts(&next_st, "{\"buf\":");
    sb_put_json_strn(&next_st, buf.data + pos, buf.len - pos);
    sb_puts(&next_st, ",\"id\":");
    sb_put_json_strn(&next_st, id, idl);
    sb_puts(&next_st, ",\"model\":");
    sb_put_json_strn(&next_st, model, ml);
    if (finish_r && frl > 0) {
        sb_puts(&next_st, ",\"finish\":");
        sb_put_json_strn(&next_st, finish_r, frl);
    }
    sb_puts(&next_st, ",\"sent_role\":");
    sb_puts(&next_st, sent_role ? "true" : "false");
    sb_puts(&next_st, ",\"in\":"); sb_put_i64(&next_st, in_tok);
    sb_puts(&next_st, ",\"out\":"); sb_put_i64(&next_st, out_tok);
    sb_puts(&next_st, ",\"cr\":"); sb_put_i64(&next_st, cache_r);
    sb_puts(&next_st, ",\"cw\":"); sb_put_i64(&next_st, cache_w);
    sb_puts(&next_st, ",\"ti\":"); sb_put_i64(&next_st, tool_idx);
    sb_puts(&next_st, ",\"su\":"); sb_puts(&next_st, saw_usage ? "true" : "false");
    sb_puts(&next_st, ",\"done\":"); sb_puts(&next_st, completed ? "true" : "false");
    sb_putc(&next_st, '}');

    strbuf_t out;
    sb_init(&out, 512 + frames.len + next_st.len * 2);
    if (is_eof && completed && pos >= buf.len) {
        sb_puts(&out, "{\"next_session_state\":null");
    } else {
        sb_puts(&out, "{\"next_session_state\":");
        sb_put_b64(&out, (const uint8_t *)next_st.data, next_st.len);
    }
    sb_puts(&out, ",\"normalized_sse_frames\":");
    sb_putn(&out, frames.data, frames.len);
    if (saw_usage && completed) {
        sb_puts(&out, ",\"incremental_usage\":");
        emit_usage_json(&out, in_tok, out_tok, cache_r, cache_w);
    }
    sb_puts(&out, ",\"stream_completed\":");
    sb_puts(&out, completed ? "true" : "false");
    sb_putc(&out, '}');
    return pack_sb(&out);
}

WASM_EXPORT("normalize_error")
uint64_t normalize_error(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    int64_t status = jobj_i64(in, "http_status", 500);
    jval_t *hdrs = jobj_get(in, "sanitized_headers");
    uint32_t blen = 0;
    uint8_t *raw = jobj_b64(in, "sanitized_body", &blen);
    jval_t *doc = json_parse((const char *)raw, blen);
    jval_t *err_obj = jobj_get(doc, "error");
    uint32_t msgl = 0;
    const char *raw_msg = jobj_str(err_obj ? err_obj : doc, "message", &msgl);

    const char *code = "upstream_server_error";
    const char *etype = "upstream_error";
    const char *def_msg = "Upstream Anthropic service error.";
    bool retryable = false;
    bool reauth = false;
    int64_t cooldown_ms = 0;

    if (status == 401 || status == 403) {
        code = "authentication_error";
        etype = "authentication_error";
        def_msg = "Upstream Anthropic credentials were rejected.";
        reauth = true;
    } else if (status == 429) {
        code = "rate_limit_exceeded";
        etype = "rate_limit_error";
        def_msg = "Upstream Anthropic rate limit exceeded.";
        retryable = true;
        cooldown_ms = 5000;
        uint32_t ral = 0;
        const char *ra = get_sanitized_header(hdrs, "retry-after", &ral);
        if (ra && ral > 0) {
            int64_t sec = parse_i64_str(ra, ral, 5);
            if (sec > 0) cooldown_ms = sec * 1000;
        }
    } else if (status == 529 || status == 503) {
        code = "provider_overloaded";
        etype = "upstream_error";
        def_msg = "Upstream Anthropic API is temporarily overloaded.";
        retryable = true;
        cooldown_ms = 5000;
    } else if (status >= 500) {
        code = "upstream_server_error";
        etype = "upstream_error";
        def_msg = "Upstream Anthropic internal error.";
        retryable = true;
    } else if (status >= 400 && status < 500) {
        code = "invalid_request_error";
        etype = "invalid_request_error";
        def_msg = "Invalid request rejected by Anthropic.";
    }

    const char *pub_msg = sanitize_public_error_msg(raw_msg, msgl, def_msg);
    strbuf_t sb;
    sb_init(&sb, 512);
    sb_puts(&sb, "{\"canonical_code\":");
    sb_put_json_str(&sb, code);
    sb_puts(&sb, ",\"error_type\":");
    sb_put_json_str(&sb, etype);
    sb_puts(&sb, ",\"retryable_pre_stream\":");
    sb_puts(&sb, retryable ? "true" : "false");
    if (cooldown_ms > 0) {
        sb_puts(&sb, ",\"cooldown_ms\":");
        sb_put_i64(&sb, cooldown_ms);
    }
    sb_puts(&sb, ",\"trigger_connection_reauth\":");
    sb_puts(&sb, reauth ? "true" : "false");
    sb_puts(&sb, ",\"sanitized_public_message\":");
    sb_put_json_str(&sb, pub_msg);
    sb_putc(&sb, '}');
    return pack_sb(&sb);
}

static void anth_merge_usage_doc(const jval_t *doc, int64_t *in_tok, int64_t *out_tok, int64_t *cache_r, int64_t *cache_w, bool *found) {
    if (!doc || doc->kind != JK_OBJ) return;
    const jval_t *u = jobj_get(doc, "usage");
    if (!u) {
        const jval_t *msg = jobj_get(doc, "message");
        if (msg) u = jobj_get(msg, "usage");
    }
    if (!u || u->kind != JK_OBJ) return;
    int64_t iv = jobj_i64(u, "input_tokens", -1);
    int64_t ov = jobj_i64(u, "output_tokens", -1);
    if (iv < 0 && ov < 0) return;
    *found = true;
    if (iv >= 0) *in_tok = iv;
    if (ov >= 0) *out_tok = ov;
    int64_t crv = jobj_i64(u, "cache_read_input_tokens", -1);
    int64_t cwv = jobj_i64(u, "cache_creation_input_tokens", -1);
    if (crv >= 0) *cache_r = crv;
    if (cwv >= 0) *cache_w = cwv;
}

WASM_EXPORT("extract_usage")
uint64_t extract_usage(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t blen = 0;
    uint8_t *raw = jobj_b64(in, "raw_response_or_trailer_bytes", &blen);
    int64_t in_tok = -1, out_tok = -1, cache_r = 0, cache_w = 0;
    bool found = false;

    jval_t *doc = json_parse((const char *)raw, blen);
    if (doc && doc->kind == JK_OBJ) {
        anth_merge_usage_doc(doc, &in_tok, &out_tok, &cache_r, &cache_w, &found);
    }
    if (!found && raw && blen > 0) {
        const char *buf = (const char *)raw;
        uint32_t pos = 0;
        while (pos < blen) {
            uint32_t nl = pos;
            while (nl < blen && buf[nl] != '\n') nl++;
            uint32_t end = nl;
            if (end > pos && buf[end - 1] == '\r') end--;
            const char *line = buf + pos;
            uint32_t llen = end - pos;
            pos = (nl < blen) ? (nl + 1) : blen;
            while (llen > 0 && (*line == ' ' || *line == '\t')) { line++; llen--; }
            if (llen == 0 || line[0] == ':') continue;
            if (xstrstarts(line, llen, "data:")) {
                line += 5;
                llen -= 5;
                while (llen > 0 && (*line == ' ' || *line == '\t')) { line++; llen--; }
            } else if (xstrstarts(line, llen, "event:") || xstrstarts(line, llen, "id:") || xstrstarts(line, llen, "retry:")) {
                continue;
            }
            if (llen == 0 || xstreq_n(line, llen, "[DONE]")) continue;
            if (line[0] == '{') {
                jval_t *ldoc = json_parse(line, llen);
                if (ldoc && ldoc->kind == JK_OBJ) {
                    anth_merge_usage_doc(ldoc, &in_tok, &out_tok, &cache_r, &cache_w, &found);
                }
            }
        }
    }

    strbuf_t sb;
    sb_init(&sb, 256);
    if (found) {
        if (in_tok < 0) in_tok = 0;
        if (out_tok < 0) out_tok = 0;
        emit_usage_json(&sb, in_tok, out_tok, cache_r, cache_w);
    } else {
        int64_t fb_in = (jobj_i64(in, "fallback_utf8_input_bytes", 0) + 3) / 4;
        int64_t fb_out = (jobj_i64(in, "fallback_utf8_output_bytes", 0) + 3) / 4;
        sb_puts(&sb, "{\"input_tokens\":"); sb_put_i64(&sb, fb_in);
        sb_puts(&sb, ",\"output_tokens\":"); sb_put_i64(&sb, fb_out);
        sb_puts(&sb, ",\"cache_read_tokens\":0,\"cache_write_tokens\":0,\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_ESTIMATED\"}");
    }
    return pack_sb(&sb);
}

WASM_EXPORT("inspect_native_request")
uint64_t inspect_native_request(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t blen = 0;
    uint8_t *raw = jobj_b64(in, "raw_request_body", &blen);
    jval_t *doc = json_parse((const char *)raw, blen);

    uint32_t ml = 0;
    const char *model = jobj_str(doc, "model", &ml);
    bool stream = jobj_bool(doc, "stream", false);
    int64_t max_tok = jobj_i64(doc, "max_tokens", 0);

    strbuf_t sb;
    sb_init(&sb, 256);
    sb_puts(&sb, "{\"target_model\":");
    sb_put_json_strn(&sb, model ? model : "", ml);
    sb_puts(&sb, ",\"stream\":");
    sb_puts(&sb, stream ? "true" : "false");
    if (max_tok > 0) {
        sb_puts(&sb, ",\"max_output_tokens_hint\":");
        sb_put_i64(&sb, max_tok);
    }
    sb_puts(&sb, ",\"credential_injections\":");
    emit_anth_cred_inj(&sb);
    sb_putc(&sb, '}');
    return pack_sb(&sb);
}

WASM_EXPORT("evaluate_guardrail")
uint64_t evaluate_guardrail(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t pl = 0;
    const char *p_b64 = jobj_str(in, "payload_json", &pl);
    strbuf_t sb;
    sb_init(&sb, 128 + pl);
    sb_puts(&sb, "{\"allowed\":true");
    if (p_b64 && pl > 0) {
        sb_puts(&sb, ",\"redacted_payload_json\":");
        sb_put_json_strn(&sb, p_b64, pl);
    }
    sb_putc(&sb, '}');
    return pack_sb(&sb);
}
