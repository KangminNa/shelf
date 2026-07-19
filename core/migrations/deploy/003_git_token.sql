-- private 저장소용 액세스 토큰 (HTTPS clone/fetch 시 주입, 설정 파일에는 저장 안 함)
ALTER TABLE projects ADD COLUMN git_token TEXT DEFAULT '';
