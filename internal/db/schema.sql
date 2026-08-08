-- REMITT Schema for sqlc code generation
-- Extracted from migrations/001_legacy.up.sql

CREATE TABLE tUser (
      id               BIGINT AUTO_INCREMENT PRIMARY KEY
    , username         VARCHAR(50) NOT NULL UNIQUE KEY
    , passhash         CHAR(32) NOT NULL
    , contactemail     VARCHAR(150)
    , callbackserviceuri    VARCHAR(150)
    , callbackservicewsdluri VARCHAR(150)
    , callbackusername      VARCHAR(50)
    , callbackpassword      VARCHAR(50)
    , role             VARCHAR(50)
    , INDEX ( username )
);

CREATE TABLE tRole (
      id         BIGINT AUTO_INCREMENT PRIMARY KEY
    , username   VARCHAR(50) NOT NULL
    , rolename   VARCHAR(50) NOT NULL
    , CONSTRAINT UNIQUE KEY ( username, rolename )
    , FOREIGN KEY ( username ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tUserRoles (
      id         BIGINT AUTO_INCREMENT PRIMARY KEY
    , username   VARCHAR(50) NOT NULL
    , rolename   VARCHAR(50) NOT NULL
    , CONSTRAINT UNIQUE KEY ( username, rolename )
    , FOREIGN KEY ( username ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tUserConfig (
      user       VARCHAR(50) NOT NULL
    , cNamespace VARCHAR(150) NOT NULL
    , cOption    VARCHAR(50) NOT NULL
    , cValue     BLOB
    , FOREIGN KEY ( user ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tPayload (
      id               BIGINT AUTO_INCREMENT PRIMARY KEY
    , insert_stamp     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    , user             VARCHAR(50) NOT NULL
    , payload          LONGBLOB
    , originalId       VARCHAR(100)
    , renderPlugin     VARCHAR(100) NOT NULL
    , renderOption     VARCHAR(100) NOT NULL
    , transportPlugin  VARCHAR(100) NOT NULL
    , transportOption  VARCHAR(100)
    , payloadState     VARCHAR(20) DEFAULT 'valid'
    , KEY ( payloadState )
    , FOREIGN KEY ( user ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tProcessor (
      id         BIGINT AUTO_INCREMENT PRIMARY KEY
    , threadId   INT UNSIGNED NOT NULL DEFAULT 0
    , payloadId  BIGINT UNSIGNED NOT NULL
    , stage      VARCHAR(20)
    , plugin     VARCHAR(100) NOT NULL
    , tsStart    TIMESTAMP NULL DEFAULT NULL
    , tsEnd      TIMESTAMP NULL DEFAULT NULL
    , pInput     LONGBLOB
    , pOutput    LONGBLOB
    , FOREIGN KEY ( payloadId ) REFERENCES tPayload ( id ) ON DELETE CASCADE
);

CREATE TABLE tPlugins (
      plugin       VARCHAR(100) NOT NULL
    , version      VARCHAR(30) NOT NULL
    , author       VARCHAR(100) NOT NULL
    , category     VARCHAR(20) NOT NULL
    , inputFormat  VARCHAR(100)
    , outputFormat VARCHAR(100)
);

CREATE TABLE tPluginOptions (
      poption      VARCHAR(100) NOT NULL
    , plugin       VARCHAR(100) NOT NULL
    , fullname     VARCHAR(100) NOT NULL
    , version      VARCHAR(30) NOT NULL
    , author       VARCHAR(100) NOT NULL
    , category     VARCHAR(20) NOT NULL
    , inputFormat  VARCHAR(100)
    , outputFormat VARCHAR(100)
);

CREATE TABLE tPluginOptionTransform (
      poptionold   VARCHAR(100) NOT NULL
    , poption      VARCHAR(100) NOT NULL
    , plugin       VARCHAR(100) NOT NULL
);

CREATE TABLE tTranslation (
      plugin       VARCHAR(100) NOT NULL
    , inputFormat  VARCHAR(100) NOT NULL
    , outputFormat VARCHAR(100) NOT NULL
);

CREATE TABLE tJobs (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , jobSchedule  VARCHAR(50) NOT NULL
    , jobClass     VARCHAR(100) NOT NULL
    , jobEnabled   BOOL NOT NULL DEFAULT TRUE
);

CREATE TABLE tFileStore (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , user         VARCHAR(50) NOT NULL
    , stamp        TIMESTAMP NOT NULL
    , category     VARCHAR(50) NOT NULL
    , filename     VARCHAR(150) NOT NULL
    , payloadId    BIGINT UNSIGNED NOT NULL
    , processorId  BIGINT UNSIGNED NOT NULL
    , content      LONGBLOB
    , contentsize  BIGINT NOT NULL DEFAULT 0
    , CONSTRAINT UNIQUE KEY ( user, category, filename )
    , KEY ( stamp )
    , FOREIGN KEY ( payloadId ) REFERENCES tPayload ( id ) ON DELETE CASCADE
    , FOREIGN KEY ( processorId ) REFERENCES tProcessor ( id ) ON DELETE CASCADE
);

CREATE TABLE tEligibilityJobs (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , user         VARCHAR(50) NOT NULL
    , inserted     TIMESTAMP NOT NULL
    , processed    TIMESTAMP NULL DEFAULT NULL
    , plugin       VARCHAR(100) NOT NULL
    , payload      LONGBLOB
    , response     LONGBLOB
    , resubmission BOOL NOT NULL DEFAULT FALSE
    , completed    BOOL NOT NULL DEFAULT FALSE
    , FOREIGN KEY ( user ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tScooper (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , scooperClass VARCHAR(100) NOT NULL DEFAULT 'org.remitt.plugin.scooper.SftpScooper'
    , user         VARCHAR(50) NOT NULL
    , stamp        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    , host         VARCHAR(50) NOT NULL
    , path         VARCHAR(150) NOT NULL DEFAULT '/'
    , filename     VARCHAR(150) NOT NULL
    , content      LONGBLOB
    , KEY ( scooperClass, user, host, path, filename )
    , FOREIGN KEY ( user ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tKeyring (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , user         VARCHAR(50) NOT NULL
    , keyname      VARCHAR(150) NOT NULL
    , privatekey   BLOB
    , publickey    BLOB
    , KEY ( user, keyname )
    , FOREIGN KEY ( user ) REFERENCES tUser ( username ) ON DELETE CASCADE
);

CREATE TABLE tSshHostKeys (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , hostname     VARCHAR(150) NOT NULL
    , port         INT UNSIGNED NOT NULL DEFAULT 22
    , hostkey      TEXT
    , CONSTRAINT UNIQUE KEY ( hostname, port )
);

CREATE TABLE tPatch (
      id           BIGINT AUTO_INCREMENT PRIMARY KEY
    , patchName    VARCHAR(150) NOT NULL
    , stamp        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    , CONSTRAINT UNIQUE KEY ( patchName )
);
