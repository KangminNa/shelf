-- 모노레포: 저장소 하위 폴더에서 빌드할 수 있게 한다. 비어 있으면 저장소 루트.
ALTER TABLE projects ADD COLUMN build_path TEXT NOT NULL DEFAULT '';
