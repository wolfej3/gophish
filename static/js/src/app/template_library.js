let libraryTemplates = [];

const contentBadges = (t) => {
    let badges = [];
    if (t.has_html) {
        badges.push('<span class="label label-primary">HTML</span>');
    }
    if (t.has_text) {
        badges.push('<span class="label label-info">Text</span>');
    }
    if (t.attachment_count > 0) {
        badges.push(`<span class="label label-default">${t.attachment_count} file${t.attachment_count > 1 ? "s" : ""}</span>`);
    }
    return badges.join(" ") || '<span class="text-muted">—</span>';
};

const importTemplate = (name) => {
    api.library.import(name)
        .success((t) => {
            successFlash(`Imported "${escapeHtml(name)}" as "${escapeHtml(t.name)}". Find it under Email Templates.`);
            $("#previewModal").modal("hide");
        })
        .error((data) => {
            errorFlash(`Import failed: ${escapeHtml(data.responseJSON ? data.responseJSON.message : "unknown error")}`);
        });
};

const preview = (name) => {
    api.library.template(name)
        .success((t) => {
            $("#previewModalLabel").text(t.name);
            $("#previewSubject").text(t.subject || "—");
            $("#previewSender").text(t.envelope_sender || "—");
            let attach = (t.attachments || []).map(a => escapeHtml(a.name)).join(", ");
            $("#previewAttachments").text(attach || "None");
            // Render HTML in a sandboxed iframe (no scripts) so untrusted markup
            // can't run in the admin origin.
            document.getElementById("previewHtmlFrame").srcdoc = t.html || "<p class='text-muted' style='font-family:sans-serif'>No HTML body.</p>";
            $("#previewText").text(t.text || "No plaintext body.");
            $("#previewImportButton").off("click").on("click", () => importTemplate(t.name));
            $("#previewModal").modal("show");
        })
        .error((data) => {
            errorFlash(`Could not load preview: ${escapeHtml(data.responseJSON ? data.responseJSON.message : "unknown error")}`);
        });
};

const showEmpty = (message) => {
    $("#loading").hide();
    $("#libraryTable").hide();
    $("#emptyMessage").text(message);
    $("#emptyState").show();
};

const load = () => {
    $("#libraryTable").hide();
    $("#emptyState").hide();
    $("#loading").show();
    api.library.templates()
        .success((templates) => {
            libraryTemplates = templates || [];
            $("#loading").hide();
            if (libraryTemplates.length === 0) {
                showEmpty("The template library is configured but empty. Add templates to the git repository, then refresh.");
                return;
            }
            $("#emptyState").hide();
            $("#libraryTable").show();
            let table = $("#libraryTable").DataTable({
                destroy: true,
                columnDefs: [{ orderable: false, targets: "no-sort" }]
            });
            table.clear();
            $.each(libraryTemplates, (i, t) => {
                table.row.add([
                    escapeHtml(t.name),
                    escapeHtml(t.category || "—"),
                    escapeHtml(t.subject || "—"),
                    contentBadges(t),
                    `
                      <div class="pull-right">
                        <button class="btn btn-default preview_button" data-name="${escapeHtml(t.name)}">
                          <i class="fa fa-eye"></i> Preview
                        </button>
                        <button class="btn btn-primary import_button" data-name="${escapeHtml(t.name)}">
                          <i class="fa fa-download"></i> Import
                        </button>
                      </div>
                    `
                ]).draw();
            });
        })
        .error((data) => {
            let msg = data.responseJSON ? data.responseJSON.message : "Error reading the template library";
            if (data.status === 404) {
                showEmpty("No template library is configured. Set \"template_library\" in config.json to point at a git repository.");
            } else {
                showEmpty(msg);
            }
        });
};

const refresh = () => {
    let $btn = $("#refresh_button").prop("disabled", true);
    api.library.refresh()
        .success(() => {
            successFlashFade("Template library refreshed", 2);
            load();
        })
        .error((data) => {
            errorFlash(`Refresh failed: ${escapeHtml(data.responseJSON ? data.responseJSON.message : "unknown error")}`);
        })
        .always(() => {
            $btn.prop("disabled", false);
        });
};

$(document).ready(function () {
    load();
    $("#refresh_button").on("click", refresh);
    $("#libraryTable").on("click", ".preview_button", function () {
        preview($(this).attr("data-name"));
    });
    $("#libraryTable").on("click", ".import_button", function () {
        importTemplate($(this).attr("data-name"));
    });
});
