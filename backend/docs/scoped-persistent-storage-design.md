# Scoped Persistent Storage Design

## Goal

ユーザーまたはチームのscopeで共有でき、Sessionの生存期間から独立した永続Storageリソースを
提供する。Storageはobject storage上のfile setとして保存し、Session起動時にローカルへ展開、
実行中と終了時に変更を同期する。

共有PVCは提供しない。巨大ファイルや厳密なPOSIX filesystem consistencyを必要とする用途は
このStorageの対象外とする。

## Resource model

Storageは次の情報を持つ。

- ID、名前、説明
- `user` または `team` の所有scope
- kind: `object_snapshot`
- optionalなPool/provider制約
- state、data revision、file manifest
- 作成・更新日時

SessionはStorage ID、絶対mount path、read-only flagをattachmentとして指定する。同じSession内の
Storage ID重複、mount path重複、system pathへのmountは拒否する。解決済みattachmentとmanifestは
Session settingsへ保存し、Pod再起動でも同じStorageを復元する。

Session Profileの`config.params.storages`にも同じattachmentを指定できる。Profileをベースとし、
起動リクエストに同じ`storage_id`があればそのattachmentを置換する。異なるIDのattachmentは
Profileの順序を保ったまま残し、リクエスト固有のものを末尾へ追加する。

## Authorization

- user scopeは所有ユーザーのSessionだけがattach・読み書きできる。
- team scopeは対象teamのメンバーが利用できる。
- StorageのscopeとSessionのscopeは一致しなければならない。
- Poolが明示されたStorageは異なるPoolのSessionへattachできない。

## Object layout

contentはSHA-256でaddressするimmutable objectとして保存する。

```text
<session-persistence-prefix>/storages/<storage-id>/blobs/sha256/<digest>
```

path、digest、size、mode、削除tombstone、revisionを含むmanifestはresource KV backendへ保存する。
同じcontentの再送は同じobject keyとなる。

## Sync and last-write-wins

Session起動前にmanifestの全fileをEmptyDirへ復元する。provisionerは10秒間隔でfull scanし、変更を
file単位で親APIへ送る。graceful shutdown時には最大60秒のfinal flushを行う。

複数writerによる同一pathの更新はmergeしない。metadata commit時にserverが単調増加revisionを
割り当て、後からcommitされたwriteを採用する。削除もtombstoneを持つwriteとして同じ規則に
従う。異なるpathの更新はそれぞれmanifestへ残る。

remote更新をpullするとき、同じpathに未送信のlocal変更があればremote内容で上書きしない。
local変更をuploadしたcommitが後のrevisionとなり、そのwriteが勝つ。これは共有filesystemの
即時整合性ではなく、最大で同期周期分遅延するeventual consistencyである。

## API

```text
POST   /storages
GET    /storages?scope=user|team&team_id=...
GET    /storages/:id
PATCH  /storages/:id
DELETE /storages/:id
GET    /storages/:id/files
PUT    /storages/:id/files/*
GET    /storages/:id/files/*
DELETE /storages/:id/files/*
```

file pathはnormalized relative pathのみ許可する。1 fileの上限は64 MiBとする。

## Lifecycle and current limitations

Storageはmetadata作成後すぐに`ready`となる。Session作成時に認可、state、Pool、mount pathを検証し、
EmptyDirをmountしてからcontentを復元する。Storage付きSessionはvolumeを持たないstock Podを
採用しない。

現在のdeleteはmetadataを削除する。immutable blobは即時削除せず、orphan GCを後続実装とする。
active attachment tracking、provider advertisement、quota、明示flush APIも後続範囲である。

## Verification targets

- user/team scopeの認可とPool制約
- mount path traversal、重複、reserved pathの拒否
- 正常restore、checksum mismatch、終了時flush
- 同一pathでは後のrevisionが勝つこと
- 異なるpathの同時更新が両方残ること
- delete後のwriteでfileが復活できること
- filesystem eventを失ってもfull scanで変更を回収すること
- Session再起動時にattachmentが維持されること
