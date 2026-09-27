#ifndef GEXTTO_LT_BRIDGE_H
#define GEXTTO_LT_BRIDGE_H

// Shared ABI between the C++ libtorrent bridge (native/libtorrent_bridge.cpp
// and the package-root libtorrent_bridge.cpp compiled by cgo) and the cgo
// preamble. The struct definitions are copied from the bridge's original
// `extern "C"` block; the function declarations mirror its extern "C" API.
//
// The header must stay valid C (cgo compiles the preamble with the C
// compiler), hence `struct gextto_lt_session` is always spelled out and
// `extern "C"` is guarded.

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

struct gextto_lt_event {
    int kind;
    char hash[65];
    char name[512];
    char save_path[1024];
};

struct gextto_lt_peer {
    char address[96];
    char client[256];
    int download_rate;
    int upload_rate;
    int num_pieces;
    int seed;
};

struct gextto_lt_tracker {
    char url[640];
    int tier;
    int status;
};

struct gextto_lt_file {
    char path[1024];
    long long size;
    long long downloaded;
    int priority;
};

struct gextto_lt_status {
    char hash[65];
    char name[512];
    char save_path[1024];
    double progress;
    int state;
    int paused;
    int download_rate;
    int upload_rate;
    int num_peers;
    int num_seeds;
    int download_limit;
    int upload_limit;
    long long all_time_upload;
    long long all_time_download;
    long long seeding_seconds;
    int queue_position;
    int has_metadata;
    int auto_managed;
    int torrent_version;
    long long total_size;
    long long total_done;
};

typedef struct gextto_lt_session gextto_lt_session;
typedef struct gextto_lt_event gextto_lt_event;
typedef struct gextto_lt_peer gextto_lt_peer;
typedef struct gextto_lt_tracker gextto_lt_tracker;
typedef struct gextto_lt_file gextto_lt_file;
typedef struct gextto_lt_status gextto_lt_status;

struct gextto_lt_session* gextto_lt_create(unsigned short port_min, unsigned short port_max,
    int download_limit, int upload_limit, int active_downloads, int active_seeds,
    int active_limit, int connections_limit, unsigned char dht, unsigned char pex,
    unsigned char lsd, unsigned char upnp, unsigned char natpmp, char* error, size_t error_size);
void gextto_lt_destroy(struct gextto_lt_session* session);
void gextto_lt_version(char* output, size_t output_size);
int gextto_lt_load_ipfilter(struct gextto_lt_session* session, const char* path, int* rules_out,
    char* error, size_t error_size);
int gextto_lt_save_torrent(struct gextto_lt_session* session, const char* hash, const char* path,
    char* error, size_t error_size);
int gextto_lt_apply_settings(struct gextto_lt_session* session, const char* settings, char* error,
    size_t error_size);
int gextto_lt_add(struct gextto_lt_session* session, const char* magnet, const char* save_path,
    char* error, size_t error_size);
int gextto_lt_add_file(struct gextto_lt_session* session, const char* torrent_path,
    const char* save_path, char* hash, size_t hash_size, char* error, size_t error_size);
int gextto_lt_add_ex(struct gextto_lt_session* session, const char* magnet, const char* save_path,
    int flags, char* error, size_t error_size);
int gextto_lt_add_file_ex(struct gextto_lt_session* session, const char* torrent_path,
    const char* save_path, int flags, char* hash, size_t hash_size, char* error, size_t error_size);
unsigned int gextto_lt_torrent_count(const struct gextto_lt_session* session);
size_t gextto_lt_statuses(const struct gextto_lt_session* session, struct gextto_lt_status* output,
    size_t capacity);
size_t gextto_lt_events(struct gextto_lt_session* session, struct gextto_lt_event* output,
    size_t capacity);
void gextto_lt_promote_metadata(struct gextto_lt_session* session);
size_t gextto_lt_ensure_auto_managed(struct gextto_lt_session* session);
void gextto_lt_adjust_queue(struct gextto_lt_session* session, int enabled, int static_downloads,
    int minimum, int maximum, int static_seeds, int static_limit, int global_download_limit);
size_t gextto_lt_peers(struct gextto_lt_session* session, const char* hash,
    struct gextto_lt_peer* output, size_t capacity, char* error, size_t error_size);
size_t gextto_lt_trackers(struct gextto_lt_session* session, const char* hash,
    struct gextto_lt_tracker* output, size_t capacity, char* error, size_t error_size);
size_t gextto_lt_files(struct gextto_lt_session* session, const char* hash,
    struct gextto_lt_file* output, size_t capacity, char* error, size_t error_size);
int gextto_lt_move_storage(struct gextto_lt_session* session, const char* hash,
    const char* destination, char* error, size_t error_size);
int gextto_lt_set_paused(struct gextto_lt_session* session, const char* hash, int paused,
    char* error, size_t error_size);
int gextto_lt_set_pin(struct gextto_lt_session* session, const char* hash, int pinned,
    char* error, size_t error_size);
int gextto_lt_set_sequential(struct gextto_lt_session* session, int enabled, char* error,
    size_t error_size);
int gextto_lt_set_torrent_sequential(struct gextto_lt_session* session, const char* hash,
    int enabled, char* error, size_t error_size);
int gextto_lt_queue_top(struct gextto_lt_session* session, const char* hash, char* error,
    size_t error_size);
int gextto_lt_set_first_last(struct gextto_lt_session* session, const char* hash, int enabled,
    char* error, size_t error_size);
int gextto_lt_set_file_priorities(struct gextto_lt_session* session, const char* hash,
    const int* priorities, size_t count, char* error, size_t error_size);
int gextto_lt_add_web_seeds(struct gextto_lt_session* session, const char* hash, const char* urls,
    int remove, char* error, size_t error_size);
int gextto_lt_set_trackers(struct gextto_lt_session* session, const char* hash, const char* tiered,
    char* error, size_t error_size);
int gextto_lt_set_super_seeding(struct gextto_lt_session* session, const char* hash, int enabled,
    char* error, size_t error_size);
int gextto_lt_remove(struct gextto_lt_session* session, const char* hash, int delete_files,
    char* error, size_t error_size);
int gextto_lt_force_recheck(struct gextto_lt_session* session, const char* hash, char* error,
    size_t error_size);
int gextto_lt_reannounce(struct gextto_lt_session* session, const char* hash, char* error,
    size_t error_size);
int gextto_lt_set_limits(struct gextto_lt_session* session, const char* hash, int download_limit,
    int upload_limit, char* error, size_t error_size);
size_t gextto_lt_restore(struct gextto_lt_session* session, const char* state_dir, char* error,
    size_t error_size);
int gextto_lt_save_resume(struct gextto_lt_session* session, const char* state_dir, char* error,
    size_t error_size);

#ifdef __cplusplus

void gextto_trim_memory(void);
}
#endif

#endif
