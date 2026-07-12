# Blog Module

## Purpose

Markdown blog with posts, tags, and publishing workflow.

## Data (schema)

Tables in `data/blog.db`:
- `posts` — id, title, slug (unique), content, published, created_at, updated_at
- `tags` — id, name (unique)
- `post_tags` — post_id, tag_id (junction)

## API Endpoints

- `GET /api/blog/posts` — List posts (paginated: ?page=&limit=)
- `GET /api/blog/posts/:id` — Get single post
- `POST /api/blog/posts` — Create post (body: { title, content, published? })
- `PATCH /api/blog/posts/:id` — Update post
- `DELETE /api/blog/posts/:id` — Delete post

## Pages

- `GET /blog` — HTML page listing all posts

## Events Emitted

- `blog:post-created` — { id, title, slug }

## Dependencies

- Core services: db, events, log
- No external APIs
- No other module dependencies
