# Forum realtime comments

## Android

- Android topic pages reuse the app's existing WuKongIM connection for realtime comment events.
- No extra SSE or WebSocket connection is opened by the forum page.
- The page registers a short-lived viewing lease while visible and refreshes it every 3 minutes; leases expire after 6 minutes.
- The lease key includes UID + page session ID so two devices/pages on the same account do not unregister each other.
- Only active viewers of the affected topic are targeted. Recipients are deduplicated by UID and sent in bounded batches.
- Realtime CMD messages are non-persistent, have no red dot, and do not update the chat conversation list. REST/MySQL remain the source of truth.
- Configure `BBSGO_WUKONGIM_API_URL` with the internal WuKongIM HTTP API address to enable Android realtime delivery. If it is unset or unavailable, normal REST comments still work.

## Web

- Web comments are intentionally REST-only. No SSE/EventSource connection is created.
- Creating a comment updates the current page immediately from the create response; comments created by other users appear after the normal page reload/navigation.

## Comment layout

- Root comments are paged 20 at a time.
- Each root comment previews up to 3 child replies in one compact gray block.
- Preview replies omit avatar, time, like controls and media players to keep the topic list light.
- Opening the reply thread uses the full reply UI (avatar, time, likes, reply action, image/voice content) and pages 10 replies at a time.
- `onlyOwner=1` filters root comments at the database query layer and hides child previews, matching Tieba-style "only OP" behavior.

## Web community image compression

- Topic images, comment images, article covers and rich-text/Markdown editor images share one browser-side compressor before `/api/upload`.
- Static JPEG/PNG/WebP images are normalized to WebP, longest edge <= 1280 px, target <= 100 KiB and hard ceiling <= 110 KiB.
- Quality starts at 0.82 and falls to 0.18; if needed, dimensions shrink by 20% per pass down to a 480 px long edge.
- Animated GIF is intentionally kept as GIF so animation is not destroyed; the server's normal upload size/type validation still applies.
- `createImageBitmap` is preferred; an HTMLImageElement fallback keeps compression working in older browsers/WebViews.

## Topic hard delete

- New topic deletes physically remove the topic row in the same transaction as its owned database graph.
- Removed rows include topic tags, root comments and child replies, topic/comment likes, favorites, user feeds, reports, votes/options/records, attachment rows/download logs, and stale site messages that point at the topic.
- Published commenters' `comment_count` values are decremented safely. The topic author's `topic_count` is decremented only when the row had not already been soft-deleted by an older release.
- QA bounty refund/reward ledgers remain authoritative and are not erased. A held bounty is refunded once; a bounty already paid to an accepted answer is not refunded again.
- Score logs and operation logs are retained as accounting/audit history. Admin deletion may create a fresh deletion notice after commit.
- Legacy soft-deleted rows can be physically purged by calling delete again; the old undelete endpoint remains only for legacy rows that have not been hard-deleted.
- Forum-owned media is physically deleted after the database transaction commits. Local files, Aliyun OSS, Tencent COS and AWS S3 objects use the bbs-go uploader directly; Android voice comments use the TangSeng file service deletion bridge.
- Storage deletion is durable, not best-effort: the transaction inserts `t_storage_delete_task` rows, an immediate worker tries deletion after commit, and the scheduler retries failures every minute with exponential backoff capped at one hour.
- Before removing an object, the worker checks all still-live topic/comment/article/attachment references. A shared object is preserved while another live record uses it and is deleted when its last live owner is removed. Soft-deleted comments do not keep their own media alive.
- Configure `BBSGO_TANGSENG_API_URL` with the internal TangSengDaoDao API base URL for voice deletion. `BBSGO_TALKAMI_HMAC_SECRET` must equal TangSengDaoDao `FORUM_TOKEN_HMAC_SECRET`; the deletion endpoint only accepts HMAC-signed `common/forum/*` paths.
## Storage cleanup recheck

- Topic hard delete queues every forum-owned image/attachment and Android forum voice object before the database graph is removed.
- Topic edit now queues the pre-edit media set as well. The worker checks surviving references after commit, so unchanged media is retained while removed inline images and detached attachments are physically reclaimed.
- If attachment object upload succeeds but the attachment DB row cannot be created, the uploaded object is rolled back immediately.
- Removed/deleted attachments cannot be rebound by stale attachment IDs.


## Final deletion/concurrency recheck (2026-08-23)

- Article deletion now follows the same irreversible physical-delete policy as topic deletion: article row, tags, comments/replies, likes, favorites, reports, feed rows and stale article notifications are removed in one transaction.
- Article cover/inline media and comment media are enqueued before the database graph is deleted; edit also reclaims removed cover/inline media after commit.
- Topic/article comment creation now locks the root entity before writing. Nested replies use root -> parent lock ordering so a hard delete cannot race in a late orphan reply.
- Topic/article favorite creation uses the same root row lock, preventing a stale page from recreating a favorite after physical deletion.
- Editing a topic reclaims detached attachments/removed inline media, and a failed attachment DB insert rolls back the just-uploaded object.
