# DBeaver e CloudBeaver Community 26.2.5

Copyright (C) DBeaver Corp e colaboradores. Os dois projetos são distribuídos sob Apache License, Version 2.0. As cópias da licença, os avisos de terceiros e os cabeçalhos originais permanecem nos arquivos-fonte. A licença MIT na raiz se aplica ao código do Cloudforge.

| Projeto | Revisão | Fonte incluída | Licença |
| --- | --- | --- | --- |
| [DBeaver Community](https://github.com/dbeaver/dbeaver) | `7a9cceb4b5c05698158306eec580b93db58cdfc5` | `dbeaver-26.2.5-source.zip` | [LICENSE-DBeaver.md](LICENSE-DBeaver.md) |
| [CloudBeaver Community](https://github.com/dbeaver/cloudbeaver) | `2e549d604809f058d1c23029ec939ca7f7f8775d` | `cloudbeaver-26.2.5-source.zip` | [LICENSE-CloudBeaver](LICENSE-CloudBeaver) |

Os ZIPs contêm os arquivos rastreados nas revisões listadas. Eles podem ser extraídos lado a lado como `dbeaver` e `cloudbeaver` para seguir o [processo de build oficial do CloudBeaver](https://github.com/dbeaver/cloudbeaver/wiki/Build-and-deploy). O Cloudforge usa a imagem oficial Linux x64 da mesma versão, fixada em [dbeaver-release.json](../../apps/desktop/electron/dbeaver-release.json). A imagem mantém os componentes e avisos da publicação original, incluindo as licenças do Java em `/opt/java/openjdk/legal` e as licenças dos pacotes do sistema em `/usr/share/doc`.

As alterações do Cloudforge são a integração com o Electron, o uso da rede e das credenciais da instância Cloudforge e a [folha de estilo aplicada à interface](../../apps/desktop/electron/dbeaver-theme.css). O runtime usa os plug-ins de servidor do DBeaver com a interface web do CloudBeaver. O ZIP do DBeaver inclui também os fontes da interface desktop Eclipse RCP.
