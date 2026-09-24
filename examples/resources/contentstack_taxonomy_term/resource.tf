resource "contentstack_taxonomy" "topics" {
  uid  = "topics"
  name = "Topics"

  lifecycle {
    prevent_destroy = true
  }
}

resource "contentstack_taxonomy_term" "parent" {
  taxonomy_uid = contentstack_taxonomy.topics.uid
  uid          = "parent"
  name         = "Parent topic"

  lifecycle {
    prevent_destroy = true
  }
}

resource "contentstack_taxonomy_term" "child" {
  taxonomy_uid = contentstack_taxonomy.topics.uid
  uid          = "child"
  name         = "Child topic"
  parent_uid   = contentstack_taxonomy_term.parent.uid

  lifecycle {
    prevent_destroy = true
  }
}
