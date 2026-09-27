# Gextto UI

Leptos single-page application for Gextto, served as a static bundle by the
daemon (see the repository root README).

```bash
rustup target add wasm32-unknown-unknown
cargo install cargo-leptos
cargo leptos build --frontend-only
```

The generated bundle is `ui/target/site/pkg`, which the daemon serves.
