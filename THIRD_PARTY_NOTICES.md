# Third-party notices for the Linux RDP package

The Jianmen Linux RDP package embeds an unmodified runtime filesystem from the pinned
`guacamole/guacd:1.6.0` official container image. It includes Apache Guacamole, FreeRDP, and
their runtime dependencies.

## Primary components

| Component | License | Source |
|---|---|---|
| Apache Guacamole / guacd 1.6.0 | Apache License 2.0 | https://guacamole.apache.org/ |
| FreeRDP | Apache License 2.0 | https://github.com/FreeRDP/FreeRDP |

The Apache License 2.0 text is available at https://www.apache.org/licenses/LICENSE-2.0.
The embedded runtime retains the Alpine package database at `/lib/apk/db/installed`, including
the package names, versions, origins, and SPDX license identifiers for the complete dependency
closure. Guacamole's dependency manifest is retained at `/opt/guacamole/DEPENDENCIES`.

The runtime includes components under additional permissive and weak-copyleft licenses, including
LGPL and MPL alternatives. Jianmen does not modify those runtime libraries and loads them as
separate shared objects. Downstream distributors remain responsible for preserving the notices,
license texts, and corresponding-source obligations applicable to their distribution channel.

Apache Guacamole and related names and logos are trademarks of the Apache Software Foundation.
Their inclusion does not imply endorsement of Jianmen.
