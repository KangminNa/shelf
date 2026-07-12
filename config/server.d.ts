export declare const SERVER: {
    readonly PORT: number;
    readonly HOST: string;
    readonly NODE_ENV: string;
};
export declare const PATHS: {
    readonly DATA_DIR: string;
    readonly PLUGINS_DIR: string;
    readonly STORAGE_DIR: string;
};
export declare const AUTH: {
    readonly SESSION_TTL_SECONDS: number;
    readonly SESSION_COOKIE_NAME: "shelf_session";
    readonly BCRYPT_ROUNDS: 12;
};
export declare const LIMITS: {
    readonly RATE_LIMIT_WINDOW_MS: 60000;
    readonly RATE_LIMIT_MAX_REQUESTS: 60;
    readonly REQUEST_TIMEOUT_MS: 30000;
    readonly MAX_BODY_SIZE_BYTES: number;
    readonly MAX_UPLOAD_SIZE_BYTES: number;
    readonly PAGINATION_DEFAULT_LIMIT: 20;
    readonly PAGINATION_MAX_LIMIT: 100;
};
export declare const DB: {
    readonly PRAGMA_JOURNAL_MODE: "WAL";
    readonly PRAGMA_FOREIGN_KEYS: "ON";
    readonly PRAGMA_BUSY_TIMEOUT: 5000;
};
//# sourceMappingURL=server.d.ts.map