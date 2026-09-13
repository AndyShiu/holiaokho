set -e; cd /work; export CARGO_HOME=/work/.cargo
mkdir -p $CARGO_HOME && cat > $CARGO_HOME/config.toml <<'X'
[registries.hl]
index = "sparse+http://host.docker.internal:18081/repository/crates-group/index/"
[source.crates-io]
replace-with = "hl"
[source.hl]
registry = "sparse+http://host.docker.internal:18081/repository/crates-group/index/"
[net]
retry = 1
X
echo "== build app using a crates.io dep via group"
cargo new --quiet app && cd app && cargo add --quiet itoa@1.0.11 2>&1 | tail -1
cat > src/main.rs <<'X'
fn main() { println!("itoa says {}", itoa::Buffer::new().format(42u32)); }
X
cargo run --quiet 2>&1 | tail -1
echo "== publish own crate to hosted"
cd /work && cargo new --quiet --lib hlcrate && cd hlcrate
sed -i 's/^\[dependencies\]/description = "e2e crate"\nlicense = "MIT"\n[dependencies]/' Cargo.toml
echo 'pub fn hi() -> &'"'"'static str { "hi from hlcrate" }' > src/lib.rs
cargo publish --quiet --registry hl --token $HL_TOKEN --allow-dirty 2>&1 | grep -vE "^\s*(Updating|Packaging|Verifying|Compiling|Finished|Packaged|Uploading|Uploaded|note:)" | tail -2 || true
echo "== duplicate publish (allow_once)"
cargo publish --quiet --registry hl --token $HL_TOKEN --allow-dirty 2>&1 | grep -iE "409|already|error" | head -1 || true
echo "== consume own crate via group"
cd /work/app && cargo add --quiet hlcrate@0.1.0 2>&1 | tail -1
cat > src/main.rs <<'X'
fn main() { println!("{} / itoa {}", hlcrate::hi(), itoa::Buffer::new().format(7u8)); }
X
cargo run --quiet 2>&1 | tail -1
