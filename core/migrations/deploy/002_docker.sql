-- Docker 런타임 전환: 앱 = 컨테이너
ALTER TABLE projects ADD COLUMN source_type TEXT NOT NULL DEFAULT 'git';   -- git | image
ALTER TABLE projects ADD COLUMN image TEXT DEFAULT '';                     -- source_type=image일 때 이미지명
ALTER TABLE projects ADD COLUMN container_port INTEGER;                    -- 컨테이너 내부 포트
ALTER TABLE projects ADD COLUMN volumes TEXT DEFAULT '';                   -- host:container 줄 단위
