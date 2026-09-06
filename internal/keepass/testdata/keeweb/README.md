# Synthetic KeeWeb fixtures

All databases were generated with **kdbxweb 1.14.4**, the library behind
KeeWeb. Their master password is **`123`**. They contain only dummy data,
not data copied from a user's password database.

Sources:

- `group-first-3.1.kdbx`: `tests/kdbx3/group-first.kdbx` in
  [gokeepasslib v3.7.0](https://github.com/tobischo/gokeepasslib/tree/v3.7.0/tests).
- `protected-binary-3.1.kdbx` and `protected-binary-4.kdbx`:
  `tests/kdbx3/protected-binary.kdbx` and `tests/kdbx4/protected-binary.kdbx`
  from that same release.
- `empty-times-3.1.kdbx`: `tests/kdbx3/kdbxweb-empty-times.kdbx` in
  [the existing keetui fork](https://github.com/rmarchei/gokeepasslib/tree/87c952ea4ce2/tests/kdbx3).

The first three fixtures contain five entries across the root and nested
groups, an empty password, password history, and a protected `otp` field.
The protected-binary fixtures also contain `a.bin`: sixteen `0x07` bytes.

The empty-times fixture contains six entries, including empty timestamp
elements in metadata, entries and history. Expected passwords are specified
in `../../keeweb_test.go`.

Tests save only to temporary directories; these source fixtures stay unchanged.
