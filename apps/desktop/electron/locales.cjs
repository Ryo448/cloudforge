const { app } = require("electron");

const dialogs = {
  "pt-BR": {
    importBackup: "Importar backup",
    backupFiles: "Arquivos de backup",
    downloadSnapshot: "Baixar snapshot",
    chooseDirectory: "Selecionar pasta do projeto",
    importEnvironment: "Importar arquivo de variáveis",
    exportEnvironment: "Exportar variáveis",
  },
  "en-US": {
    importBackup: "Import backup",
    backupFiles: "Backup files",
    downloadSnapshot: "Download snapshot",
    chooseDirectory: "Select project folder",
    importEnvironment: "Import environment file",
    exportEnvironment: "Export variables",
  },
  es: {
    importBackup: "Importar copia de seguridad",
    backupFiles: "Archivos de respaldo",
    downloadSnapshot: "Descargar snapshot",
    chooseDirectory: "Seleccionar carpeta del proyecto",
    importEnvironment: "Importar archivo de variables",
    exportEnvironment: "Exportar variables",
  },
  de: {
    importBackup: "Sicherung importieren",
    backupFiles: "Sicherungsdateien",
    downloadSnapshot: "Snapshot herunterladen",
    chooseDirectory: "Projektordner auswählen",
    importEnvironment: "Umgebungsdatei importieren",
    exportEnvironment: "Variablen exportieren",
  },
  fr: {
    importBackup: "Importer une sauvegarde",
    backupFiles: "Fichiers de sauvegarde",
    downloadSnapshot: "Télécharger l’instantané",
    chooseDirectory: "Sélectionner le dossier du projet",
    importEnvironment: "Importer le fichier de variables",
    exportEnvironment: "Exporter les variables",
  },
  it: {
    importBackup: "Importa backup",
    backupFiles: "File di backup",
    downloadSnapshot: "Scarica snapshot",
    chooseDirectory: "Seleziona la cartella del progetto",
    importEnvironment: "Importa file delle variabili",
    exportEnvironment: "Esporta variabili",
  },
  "zh-CN": {
    importBackup: "导入备份",
    backupFiles: "备份文件",
    downloadSnapshot: "下载快照",
    chooseDirectory: "选择项目文件夹",
    importEnvironment: "导入环境变量文件",
    exportEnvironment: "导出变量",
  },
  ja: {
    importBackup: "バックアップをインポート",
    backupFiles: "バックアップファイル",
    downloadSnapshot: "スナップショットをダウンロード",
    chooseDirectory: "プロジェクトフォルダーを選択",
    importEnvironment: "環境変数ファイルをインポート",
    exportEnvironment: "変数をエクスポート",
  },
};

function systemLanguageArguments() {
  return [
    "--cloudforge-languages=" + app.getPreferredSystemLanguages().join(","),
  ];
}

module.exports = { dialogs, systemLanguageArguments };
